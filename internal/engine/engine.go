// Package engine decides what a shell's environment should be for its current
// directory. It is shell-agnostic: it returns a list of variable changes that
// the shell package renders as export/unset statements.
package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/thomaschaplin/kdir/internal/discover"
	"github.com/thomaschaplin/kdir/internal/kubeconfig"
	"github.com/thomaschaplin/kdir/internal/overlay"
	"github.com/thomaschaplin/kdir/internal/resolve"
	"github.com/thomaschaplin/kdir/internal/spec"
	"github.com/thomaschaplin/kdir/internal/state"
	"github.com/thomaschaplin/kdir/internal/trust"
)

// Environment variables kdir manages.
const (
	EnvKubeconfig = "KUBECONFIG"
	EnvOrig       = "KDIR_ORIG_KUBECONFIG"
	EnvOrigSet    = "KDIR_ORIG_KUBECONFIG_SET"
	EnvState      = "KDIR_STATE"
	EnvOverlay    = "KDIR_OVERLAY"
	EnvRoot       = "KDIR_ROOT"
	EnvContext    = "KDIR_CONTEXT"
	EnvNamespace  = "KDIR_NAMESPACE"
	EnvProtect    = "KDIR_PROTECT"
)

// managedVars are unset when the shell leaves a governed tree.
var managedVars = []string{EnvOrig, EnvOrigSet, EnvState, EnvOverlay, EnvRoot, EnvContext, EnvNamespace, EnvProtect}

// Env is everything the engine reads from the outside world, so tests can
// fake it.
type Env struct {
	LookupEnv func(string) (string, bool)
	Dir       string // current directory (logical $PWD)
	Home      string
	PID       int // PID of the shell being managed
}

// Getenv returns the variable's value or "".
func (e Env) Getenv(k string) string {
	v, _ := e.LookupEnv(k)
	return v
}

// OSEnv returns an Env for the current process.
func OSEnv(pid int) (Env, error) {
	dir, err := os.Getwd()
	if err != nil {
		return Env{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	return Env{LookupEnv: os.LookupEnv, Dir: dir, Home: home, PID: pid}, nil
}

// Change is one environment change for the shell to apply.
type Change struct {
	Name  string
	Value string
	Unset bool
}

// Result is what the shell should do.
type Result struct {
	Changes  []Change
	Messages []string // one-line notices for stderr, without the "kdir: " prefix
}

func (r *Result) set(name, value string) {
	r.Changes = append(r.Changes, Change{Name: name, Value: value})
}
func (r *Result) unset(names ...string) {
	for _, n := range names {
		r.Changes = append(r.Changes, Change{Name: n, Unset: true})
	}
}
func (r *Result) msg(format string, args ...any) {
	r.Messages = append(r.Messages, fmt.Sprintf(format, args...))
}

// Original is the user's own KUBECONFIG, before kdir changed it.
type Original struct {
	Value string
	Set   bool
}

// Files returns the kubeconfig files the original value refers to.
func (o Original) Files(home string) []string { return kubeconfig.Files(o.Value, home) }

// Kubeconfig returns the KUBECONFIG value that puts overlayPath in front of
// the original list. An unset or empty original is spelled out as
// ~/.kube/config, because KUBECONFIG=<overlay> alone would hide it.
func (o Original) Kubeconfig(overlayPath, home string) string {
	rest := o.Value
	if rest == "" {
		rest = filepath.Join(home, ".kube", "config")
	}
	return overlayPath + string(os.PathListSeparator) + rest
}

// shellState is what the engine knows about the shell before deciding.
type shellState struct {
	st       state.State
	hasState bool
	own      bool // state was written for this shell (not inherited from a parent)
	orig     Original
	managed  bool // KUBECONFIG currently points at a kdir overlay
	adopted  bool // the user changed KUBECONFIG by hand; orig was re-derived
	overlay  string
}

func readShell(e Env) shellState {
	var s shellState
	s.st, s.hasState = state.Decode(e.Getenv(EnvState))
	s.own = s.hasState && s.st.PID == e.PID
	s.overlay = e.Getenv(EnvOverlay)
	if setFlag, ok := e.LookupEnv(EnvOrigSet); ok {
		s.managed = true
		s.orig = Original{Value: e.Getenv(EnvOrig), Set: setFlag == "1"}
		if s.overlay != "" {
			cur, curSet := e.LookupEnv(EnvKubeconfig)
			if !curSet || cur != s.orig.Kubeconfig(s.overlay, e.Home) {
				// The user replaced KUBECONFIG themselves (e.g. export
				// KUBECONFIG=~/other). Respect it as the new original,
				// minus our overlay if they only appended to it.
				prefix := s.overlay + string(os.PathListSeparator)
				cur = strings.TrimPrefix(cur, prefix)
				if cur == s.overlay {
					cur, curSet = "", false
				}
				s.orig = Original{Value: cur, Set: curSet}
				s.adopted = true
			}
		}
	} else {
		v, ok := e.LookupEnv(EnvKubeconfig)
		s.orig = Original{Value: v, Set: ok}
	}
	return s
}

// Stops returns the directories discovery must not walk above.
func Stops(home string) []string {
	stops := []string{home}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		stops = append(stops, resolved)
	}
	return stops
}

// TrustStore returns the user's trust store.
func TrustStore(e Env) trust.Store {
	return trust.Store{Dir: trust.DefaultDir(e.Getenv, e.Home)}
}

// Canonical resolves symlinks in a .kubectx path so a file reached through
// different symlinked paths has one trust entry.
func Canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// Export computes the environment changes for the shell's current directory.
// When nothing changed it returns an empty Result after only a few stat calls.
func Export(e Env) (Result, error) {
	s := readShell(e)
	found, err := discover.Find(e.Dir, Stops(e.Home)...)
	if err != nil {
		return Result{}, err
	}
	if found == nil {
		if !s.managed && !s.hasState {
			return Result{}, nil
		}
		return leave(e, s), nil
	}

	store := TrustStore(e)
	fp := state.Fingerprint(state.Inputs{
		KubectxFile:     found.Path,
		TrustEntry:      store.EntryPath(Canonical(found.Path)),
		KubeconfigFiles: s.orig.Files(e.Home),
		OrigKubeconfig:  s.orig.Value,
		OrigSet:         s.orig.Set,
	})
	if s.own && !s.adopted && s.st.File == found.Path && s.st.Fingerprint == fp {
		switch s.st.Mode {
		case state.NotAllowed:
			if !s.managed {
				return Result{}, nil
			}
		case state.Active, state.FailClosed:
			if s.managed && fileExists(s.overlay) {
				return Result{}, nil
			}
		}
	}
	return enter(e, s, found, fp, store)
}

// Decision is kdir's full evaluation of a governing .kubectx file. It is
// shared by Export and `kdir status`.
type Decision struct {
	Found    *discover.Found
	Trust    trust.Status
	Spec     *spec.Spec
	Warnings []string
	SpecErr  error
	KubeErr  error
	Config   *clientcmdapi.Config
	Result   resolve.Result

	// Mode is Active when a context was resolved, FailClosed otherwise, or
	// NotAllowed when the file is not trusted (in which case nothing below
	// Trust is evaluated).
	Mode      state.Mode
	Context   string // context to select (a non-existent name when FailClosed)
	Namespace string
	Problem   string // one-line explanation when FailClosed
}

// Evaluate reads, checks and resolves a governing .kubectx file against the
// original kubeconfig. It has no side effects.
func Evaluate(e Env, found *discover.Found, orig Original, store trust.Store) (*Decision, error) {
	d := &Decision{Found: found}
	data, err := os.ReadFile(found.Path)
	if err != nil {
		return nil, err
	}
	d.Trust, err = store.Check(Canonical(found.Path), data)
	if err != nil {
		return nil, err
	}
	if d.Trust != trust.Allowed {
		d.Mode = state.NotAllowed
		return d, nil
	}
	d.Spec, d.Warnings, d.SpecErr = spec.Parse(data)
	d.Config, d.KubeErr = kubeconfig.Load(orig.Files(e.Home))
	if d.KubeErr != nil {
		d.Config = nil
	}

	rel := Tilde(found.Path, e.Home)
	switch {
	case d.SpecErr != nil:
		d.Mode, d.Context = state.FailClosed, overlay.FailClosedName(d.Config, "invalid", "")
		d.Problem = fmt.Sprintf("%s: %v. kubectl is disabled in this directory until it is fixed.", rel, d.SpecErr)
	case d.KubeErr != nil:
		d.Mode, d.Context = state.FailClosed, overlay.FailClosedName(nil, "error", "")
		d.Problem = fmt.Sprintf("can't load kubeconfig: %v. kubectl is disabled in this directory until it loads.", d.KubeErr)
	default:
		d.Result = resolve.Resolve(d.Spec, d.Config)
		switch d.Result.Status {
		case resolve.Found:
			d.Mode, d.Context, d.Namespace = state.Active, d.Result.Context, d.Result.Namespace
		case resolve.Missing:
			d.Mode, d.Context = state.FailClosed, overlay.FailClosedName(d.Config, "missing", d.Spec.Context)
			d.Problem = d.Result.Message(d.Spec)
		case resolve.Ambiguous:
			d.Mode, d.Context = state.FailClosed, overlay.FailClosedName(d.Config, "ambiguous", "")
			d.Problem = d.Result.Message(d.Spec)
		}
	}
	return d, nil
}

func enter(e Env, s shellState, found *discover.Found, fp string, store trust.Store) (Result, error) {
	var r Result
	d, err := Evaluate(e, found, s.orig, store)
	if err != nil {
		return Result{}, err
	}
	newState := state.State{PID: e.PID, Mode: d.Mode, Fingerprint: fp, File: found.Path}

	if d.Mode == state.NotAllowed {
		if s.managed {
			restore(&r, s.orig)
			r.unset(EnvOrig, EnvOrigSet, EnvOverlay, EnvRoot, EnvContext, EnvNamespace, EnvProtect)
			if s.own {
				discard(&r, s.overlay, e)
			}
		}
		r.set(EnvState, newState.Encode())
		verb := "is not allowed"
		if d.Trust == trust.Changed {
			verb = "changed since it was allowed"
		}
		r.msg(".kubectx in %s %s. Run 'kdir allow' to trust it.", Tilde(found.Dir, e.Home), verb)
		return r, nil
	}

	rel := Tilde(found.Path, e.Home)
	for _, w := range d.Warnings {
		r.msg("%s: %s", rel, w)
	}
	if d.Problem != "" {
		r.msg("%s", d.Problem)
	}

	dir, err := overlay.Dir(e.Getenv)
	if err != nil {
		return Result{}, fmt.Errorf("overlay directory: %w", err)
	}
	path := overlay.Path(dir, e.PID)
	if overlay.GCDue(dir, time.Now()) {
		// Opportunistic, at most once a day: clean up after shells that
		// exited without leaving their directory (closed terminals).
		if res, err := overlay.GC(dir); err == nil && len(res.Saved) > 0 {
			r.msg("saved %d overlay(s) from exited shells that contain credentials written by other tools; see 'kdir gc'", len(res.Saved))
		}
	}
	if s.own && s.overlay != "" && s.overlay != path {
		discard(&r, s.overlay, e)
	}
	keepCredentials(&r, path, e)
	if err := overlay.Write(path, overlay.Build(d.Config, d.Context, d.Namespace)); err != nil {
		return Result{}, fmt.Errorf("writing overlay: %w", err)
	}

	if !s.managed || s.adopted {
		r.set(EnvOrig, s.orig.Value)
		r.set(EnvOrigSet, boolFlag(s.orig.Set))
	}
	r.set(EnvKubeconfig, s.orig.Kubeconfig(path, e.Home))
	r.set(EnvOverlay, path)
	r.set(EnvRoot, found.Dir)
	if d.Mode == state.Active {
		r.set(EnvContext, d.Context)
		if d.Namespace != "" {
			r.set(EnvNamespace, d.Namespace)
		} else {
			r.unset(EnvNamespace)
		}
		if d.Spec.Protect {
			r.set(EnvProtect, "1")
		} else {
			r.unset(EnvProtect)
		}
	} else {
		r.unset(EnvContext, EnvNamespace, EnvProtect)
	}
	r.set(EnvState, newState.Encode())
	return r, nil
}

func leave(e Env, s shellState) Result {
	var r Result
	if s.managed {
		restore(&r, s.orig)
	}
	if s.own {
		discard(&r, s.overlay, e)
	}
	r.unset(managedVars...)
	return r
}

func restore(r *Result, o Original) {
	if o.Set {
		r.set(EnvKubeconfig, o.Value)
	} else {
		r.unset(EnvKubeconfig)
	}
}

// discard removes an overlay this shell owns, first saving it if another tool
// wrote credentials into it.
func discard(r *Result, path string, e Env) {
	if path == "" {
		return
	}
	keepCredentials(r, path, e)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		r.msg("can't remove overlay %s: %v", path, err)
	}
}

// keepCredentials moves an overlay aside if something added clusters or
// users to it (aws eks update-kubeconfig, gcloud get-credentials, kind and
// `kubectl config set-credentials` all write to the first KUBECONFIG file).
func keepCredentials(r *Result, path string, e Env) {
	has, err := overlay.HasCredentials(path)
	if err == nil && !has {
		return
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return
	}
	kept := filepath.Join(filepath.Dir(path), overlay.KeptName(e.PID))
	if renameErr := os.Rename(path, kept); renameErr != nil {
		r.msg("can't save overlay %s: %v", path, renameErr)
		return
	}
	if err != nil {
		r.msg("this shell's overlay could not be parsed; saved it as %s", kept)
		return
	}
	r.msg("another tool (e.g. aws eks update-kubeconfig) wrote clusters/users into this shell's overlay; saved them to %s. Merge with: KUBECONFIG=~/.kube/config:%s kubectl config view --flatten", kept, kept)
}

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// Tilde abbreviates a path under home as ~/...
func Tilde(path, home string) string {
	if home == "" || home == "/" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}
