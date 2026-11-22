package engine

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/thomaschaplin/kdir/internal/testutil"
	"github.com/thomaschaplin/kdir/internal/trust"
)

// world is a fake machine: a home directory with a kubeconfig and some
// project directories.
type world struct {
	t    *testing.T
	root string
	home string
	kube string // ~/.kube/config
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	w := &world{t: t, root: root, home: filepath.Join(root, "home")}
	w.kube = filepath.Join(w.home, ".kube", "config")
	testutil.WriteFile(t, w.kube, mustRead(t, testutil.Fixture("kubeconfig/basic.yaml")))
	return w
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (w *world) path(rel string) string { return filepath.Join(w.home, rel) }

// project writes a .kubectx in ~/rel and, if allow is set, trusts it.
func (w *world) project(rel, content string, allow bool) string {
	w.t.Helper()
	p := testutil.WriteFile(w.t, filepath.Join(w.path(rel), ".kubectx"), content)
	if allow {
		w.allow(p)
	}
	return p
}

func (w *world) store() trust.Store {
	return trust.Store{Dir: filepath.Join(w.root, "xdgconfig", "kdir", "allowed")}
}

func (w *world) allow(p string) {
	w.t.Helper()
	if err := w.store().Allow(Canonical(p), []byte(mustRead(w.t, p))); err != nil {
		w.t.Fatal(err)
	}
}

// sh is a fake shell: an environment that applies Export's changes.
type sh struct {
	w    *world
	pid  int
	dir  string
	env  map[string]string
	last Result
}

func (w *world) shell(pid int, env map[string]string) *sh {
	e := map[string]string{
		"XDG_RUNTIME_DIR": filepath.Join(w.root, "run"),
		"XDG_CONFIG_HOME": filepath.Join(w.root, "xdgconfig"),
	}
	maps.Copy(e, env)
	return &sh{w: w, pid: pid, dir: w.home, env: e}
}

// fork returns a child shell that inherits this one's environment.
func (s *sh) fork(pid int) *sh {
	return &sh{w: s.w, pid: pid, dir: s.dir, env: maps.Clone(s.env)}
}

func (s *sh) cd(rel string) *sh {
	s.w.t.Helper()
	if filepath.IsAbs(rel) {
		s.dir = rel
	} else {
		s.dir = s.w.path(rel)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		s.w.t.Fatal(err)
	}
	return s.prompt()
}

func (s *sh) prompt() *sh {
	s.w.t.Helper()
	e := Env{
		LookupEnv: func(k string) (string, bool) { v, ok := s.env[k]; return v, ok },
		Dir:       s.dir,
		Home:      s.w.home,
		PID:       s.pid,
	}
	r, err := Export(e)
	if err != nil {
		s.w.t.Fatalf("Export: %v", err)
	}
	for _, c := range r.Changes {
		if c.Unset {
			delete(s.env, c.Name)
		} else {
			s.env[c.Name] = c.Value
		}
	}
	s.last = r
	return s
}

func (s *sh) clientConfig() clientcmd.ClientConfig {
	files := []string{s.w.kube}
	if v := s.env["KUBECONFIG"]; v != "" {
		files = filepath.SplitList(v)
	}
	rules := &clientcmd.ClientConfigLoadingRules{Precedence: files}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
}

// kubectl returns "context/namespace" as kubectl would see it in this shell,
// or "ERROR" if kubectl would fail.
func (s *sh) kubectl() string {
	s.w.t.Helper()
	cc := s.clientConfig()
	if _, err := cc.ClientConfig(); err != nil {
		return "ERROR"
	}
	raw, _ := cc.RawConfig()
	ns, _, _ := cc.Namespace()
	return raw.CurrentContext + "/" + ns
}

func (s *sh) expect(want string) {
	s.w.t.Helper()
	if got := s.kubectl(); got != want {
		s.w.t.Fatalf("kubectl sees %s, want %s (KUBECONFIG=%q)", got, want, s.env["KUBECONFIG"])
	}
}

func (s *sh) expectQuiet() {
	s.w.t.Helper()
	if len(s.last.Changes) != 0 || len(s.last.Messages) != 0 {
		s.w.t.Fatalf("expected no-op prompt, got %+v", s.last)
	}
}

func (s *sh) expectMessage(substr string) {
	s.w.t.Helper()
	for _, m := range s.last.Messages {
		if strings.Contains(m, substr) {
			return
		}
	}
	s.w.t.Fatalf("no message containing %q in %q", substr, s.last.Messages)
}

func (s *sh) useContext(name string) {
	s.w.t.Helper()
	s.modify(func(c *clientcmdapi.Config) { c.CurrentContext = name })
}

func (s *sh) modify(fn func(*clientcmdapi.Config)) {
	s.w.t.Helper()
	s.w.t.Setenv("KUBECONFIG", s.env["KUBECONFIG"])
	po := clientcmd.NewDefaultPathOptions()
	cfg, err := po.GetStartingConfig()
	if err != nil {
		s.w.t.Fatal(err)
	}
	fn(cfg)
	if err := clientcmd.ModifyConfig(po, *cfg, true); err != nil {
		s.w.t.Fatal(err)
	}
}

func overlayFiles(t *testing.T, w *world) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(w.root, "run", "kdir"))
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// The core cycle: enter, nested dirs, leave. The user's environment is
// restored exactly and their kubeconfig is never written.
func TestEnterLeaveRestoresEverything(t *testing.T) {
	cases := map[string]map[string]string{
		"KUBECONFIG unset": {},
		"KUBECONFIG empty": {"KUBECONFIG": ""},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.project("src/payments", "context: staging-eu\nnamespace: payments\n", true)
			snap := testutil.TakeSnapshot(t, w.kube)
			s := w.shell(100, env)
			before := maps.Clone(s.env)

			s.cd("src").expectQuiet()
			s.expect("prod-eu/payments")
			s.cd("src/payments")
			s.expect("staging-eu/payments")
			if s.env[EnvContext] != "staging-eu" || s.env[EnvNamespace] != "payments" || s.env[EnvRoot] != w.path("src/payments") {
				t.Fatalf("prompt vars: %v", s.env)
			}
			s.prompt().expectQuiet()
			s.cd("src/payments/svc/api").expectQuiet()
			s.expect("staging-eu/payments")
			s.cd("src/payments").expectQuiet()
			s.cd("elsewhere")
			s.expect("prod-eu/payments")

			if !maps.Equal(s.env, before) {
				t.Fatalf("environment not restored:\n got %v\nwant %v", s.env, before)
			}
			if files := overlayFiles(t, w); len(files) != 0 {
				t.Fatalf("overlay files left behind: %v", files)
			}
			snap.AssertUnchanged(t)
		})
	}
}

func TestMultiFileKubeconfigRestored(t *testing.T) {
	w := newWorld(t)
	a := testutil.CopyFixture(t, "kubeconfig/multi-a.yaml", w.root)
	b := testutil.CopyFixture(t, "kubeconfig/multi-b.yaml", w.root)
	orig := a + ":" + b
	snap := testutil.TakeSnapshot(t, a, b)
	w.project("proj", "context: beta\nnamespace: web\n", true)
	s := w.shell(100, map[string]string{"KUBECONFIG": orig})
	s.expect("alpha/alpha-ns")
	s.cd("proj")
	s.expect("beta/web")
	if s.env["KUBECONFIG"] != s.env[EnvOverlay]+":"+orig {
		t.Fatalf("KUBECONFIG = %q", s.env["KUBECONFIG"])
	}
	s.cd("")
	if s.env["KUBECONFIG"] != orig {
		t.Fatalf("KUBECONFIG not restored: %q", s.env["KUBECONFIG"])
	}
	s.expect("alpha/alpha-ns")
	snap.AssertUnchanged(t)
}

func TestMoveBetweenGovernedDirs(t *testing.T) {
	w := newWorld(t)
	w.project("a", "context: staging-eu\n", true)
	w.project("b", "context: dev\nnamespace: tools\n", true)
	s := w.shell(100, map[string]string{"KUBECONFIG": w.kube})
	s.cd("a")
	s.expect("staging-eu/default")
	s.cd("b")
	s.expect("dev/tools")
	if s.env[EnvOrig] != w.kube || s.env[EnvOrigSet] != "1" {
		t.Fatalf("original KUBECONFIG overwritten: %q %q", s.env[EnvOrig], s.env[EnvOrigSet])
	}
	for _, c := range s.last.Changes {
		if c.Name == EnvOrig {
			t.Fatal("KDIR_ORIG_KUBECONFIG re-exported when moving between governed dirs")
		}
	}
	s.cd("")
	if s.env["KUBECONFIG"] != w.kube {
		t.Fatalf("KUBECONFIG = %q", s.env["KUBECONFIG"])
	}
}

func TestHomeDefault(t *testing.T) {
	w := newWorld(t)
	w.project("", "context: dev\n", true) // ~/.kubectx
	s := w.shell(100, nil)
	s.cd("anything/deep")
	s.expect("dev/default")
	// Outside $HOME it does not apply.
	s.cd(w.root)
	s.expect("prod-eu/payments")
}

func TestNotAllowed(t *testing.T) {
	w := newWorld(t)
	p := w.project("proj", "context: staging-eu\n", false)
	s := w.shell(100, nil)
	s.cd("proj")
	s.expect("prod-eu/payments")
	s.expectMessage(".kubectx in ~/proj is not allowed. Run 'kdir allow' to trust it.")
	if _, ok := s.env["KUBECONFIG"]; ok {
		t.Fatal("KUBECONFIG set for an unallowed file")
	}
	// Once per entry: the next prompts are silent.
	s.prompt().expectQuiet()
	s.cd("proj/sub").expectQuiet()

	w.allow(p)
	s.prompt()
	s.expect("staging-eu/default")

	// Leaving and re-entering an unallowed dir warns again.
	w.project("other", "context: dev\n", false)
	s.cd("other")
	s.expectMessage("not allowed")
	s.cd("")
	s.cd("other")
	s.expectMessage("not allowed")
}

func TestEditedAfterAllow(t *testing.T) {
	w := newWorld(t)
	p := w.project("proj", "context: staging-eu\n", true)
	s := w.shell(100, nil)
	s.cd("proj")
	s.expect("staging-eu/default")
	testutil.WriteFile(t, p, "context: prod-eu\n")
	s.prompt()
	s.expectMessage("changed since it was allowed")
	s.expect("prod-eu/payments") // back to the user's own config, not the edited target
	if _, ok := s.env["KUBECONFIG"]; ok {
		t.Fatal("KUBECONFIG should be restored to unset")
	}
	if files := overlayFiles(t, w); len(files) != 0 {
		t.Fatalf("overlay left: %v", files)
	}
	w.allow(p)
	s.prompt()
	s.expect("prod-eu/payments")
	if s.env[EnvContext] != "prod-eu" {
		t.Fatal("not active after re-allow")
	}
}

func TestFailClosed(t *testing.T) {
	tests := []struct {
		name, spec, msg string
	}{
		{"missing context", "context: staging-us\n",
			`.kubectx wants context "staging-us" but it isn't in your kubeconfig. kubectl is disabled in this directory until it is.`},
		{"ambiguous regex", "contextRegex: staging\n", "3 contexts match"},
		{"ambiguous server", "server: https://ABCD1234.gr7.eu-west-1.eks.amazonaws.com\n", "3 contexts match"},
		{"parse error", "context: [\n", "invalid YAML"},
		{"validation error", "namespace: x\n", "no matcher"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.project("proj", tt.spec, true)
			s := w.shell(100, nil)
			s.cd("proj")
			s.expect("ERROR")
			s.expectMessage(tt.msg)
			if len(s.last.Messages) != 1 {
				t.Errorf("want exactly one message, got %q", s.last.Messages)
			}
			if _, ok := s.env[EnvContext]; ok {
				t.Error("KDIR_CONTEXT set while fail-closed")
			}
			s.prompt().expectQuiet()
			s.cd("")
			s.expect("prod-eu/payments")
		})
	}
}

func TestFailClosedRecoversWhenContextAppears(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-us\n", true)
	s := w.shell(100, nil)
	s.cd("proj")
	s.expect("ERROR")
	data := strings.Replace(mustRead(t, w.kube), "- name: dev\n  context:", "- name: staging-us\n  context:\n    cluster: dev\n    user: dev-user\n- name: dev\n  context:", 1)
	testutil.WriteFile(t, w.kube, data)
	s.prompt()
	s.expect("staging-us/default")
}

func TestBrokenKubeconfigFailsClosed(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-eu\n", true)
	testutil.WriteFile(t, w.kube, "this: [is not a kubeconfig")
	s := w.shell(100, nil)
	s.cd("proj")
	s.expectMessage("can't load kubeconfig")
	if !strings.Contains(s.env["KUBECONFIG"], "run/kdir") {
		t.Fatal("no overlay in front of a broken kubeconfig")
	}
}

func TestDeletedOverlayIsRecreated(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-us\n", true)
	s := w.shell(100, nil)
	s.cd("proj")
	s.expect("ERROR")
	if err := os.Remove(s.env[EnvOverlay]); err != nil {
		t.Fatal(err)
	}
	s.expect("prod-eu/payments") // this is the fail-open the hook must repair
	s.prompt()
	s.expect("ERROR")
}

func TestInShellUseContextPersists(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-eu\nnamespace: payments\n", true)
	snap := testutil.TakeSnapshot(t, w.kube)
	s := w.shell(100, nil)
	s.cd("proj")
	s.useContext("dev")
	s.modify(func(c *clientcmdapi.Config) { c.Contexts["dev"].Namespace = "tools" })
	s.prompt().expectQuiet()
	s.cd("proj/sub").expectQuiet()
	s.expect("dev/tools")
	if got := Short(Env{LookupEnv: func(k string) (string, bool) { v, ok := s.env[k]; return v, ok }}); got != "dev:tools" {
		t.Fatalf("Short = %q", got)
	}
	s.cd("")
	s.expect("prod-eu/payments")
	snap.AssertUnchanged(t)
}

func TestUserChangesKubeconfigInside(t *testing.T) {
	w := newWorld(t)
	a := testutil.CopyFixture(t, "kubeconfig/multi-a.yaml", w.root)
	w.project("proj", "context: staging-eu\n", true)
	s := w.shell(100, nil)
	s.cd("proj")
	s.env["KUBECONFIG"] = a + ":" + w.kube // user: export KUBECONFIG=...
	s.prompt()
	s.expect("staging-eu/default")
	if s.env[EnvOrig] != a+":"+w.kube || s.env[EnvOrigSet] != "1" {
		t.Fatalf("new original not adopted: %q", s.env[EnvOrig])
	}
	s.cd("")
	if s.env["KUBECONFIG"] != a+":"+w.kube {
		t.Fatalf("KUBECONFIG = %q, want the user's own value", s.env["KUBECONFIG"])
	}
}

func TestChildShellGetsOwnOverlay(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-eu\n", true)
	parent := w.shell(100, nil)
	parent.cd("proj")
	parentOverlay := parent.env[EnvOverlay]

	child := parent.fork(200)
	child.prompt()
	if child.env[EnvOverlay] == parentOverlay {
		t.Fatal("child shell reuses parent's overlay")
	}
	child.useContext("dev")
	child.expect("dev/default")
	parent.expect("staging-eu/default")

	child.cd("")
	child.expect("prod-eu/payments")
	if _, err := os.Stat(parentOverlay); err != nil {
		t.Fatal("child deleted the parent's overlay")
	}
	parent.prompt().expectQuiet()
	parent.expect("staging-eu/default")
}

func TestTwoShellsIndependent(t *testing.T) {
	w := newWorld(t)
	w.project("a", "context: staging-eu\n", true)
	w.project("b", "context: dev\n", true)
	one, two := w.shell(100, nil), w.shell(200, nil)
	one.cd("a")
	two.cd("a")
	two.useContext("prod-eu")
	one.prompt().expectQuiet()
	one.expect("staging-eu/default")
	two.expect("prod-eu/payments")
	two.cd("b")
	one.expect("staging-eu/default")
	two.expect("dev/default")
	one.cd("")
	two.expect("dev/default")
}

func TestCredentialsWrittenIntoOverlayAreKept(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-eu\n", true)
	s := w.shell(100, nil)
	s.cd("proj")
	s.modify(func(c *clientcmdapi.Config) {
		c.AuthInfos["eks-new"] = &clientcmdapi.AuthInfo{Token: "fake"}
	})
	s.cd("")
	s.expectMessage("wrote clusters/users into this shell's overlay")
	files := overlayFiles(t, w)
	if len(files) != 1 || !strings.HasPrefix(files[0], "kept-100-") {
		t.Fatalf("kept file missing: %v", files)
	}
}

func TestNotGovernedIsFree(t *testing.T) {
	w := newWorld(t)
	s := w.shell(100, map[string]string{"KUBECONFIG": "/x"})
	s.cd("a/b/c").expectQuiet()
	if s.env["KUBECONFIG"] != "/x" {
		t.Fatal("touched KUBECONFIG outside governed tree")
	}
}

func TestOriginalKubeconfigKubeconfig(t *testing.T) {
	if got := (Original{}).Kubeconfig("/o.yaml", "/h"); got != "/o.yaml:/h/.kube/config" {
		t.Errorf("unset: %q", got)
	}
	if got := (Original{Value: "/a:/b", Set: true}).Kubeconfig("/o.yaml", "/h"); got != "/o.yaml:/a:/b" {
		t.Errorf("set: %q", got)
	}
}

func TestTilde(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"/home/u", "~"}, {"/home/u/src", "~/src"}, {"/home/user2", "/home/user2"}, {"/etc", "/etc"},
	} {
		if got := Tilde(tt.in, "/home/u"); got != tt.want {
			t.Errorf("Tilde(%q) = %q", tt.in, got)
		}
	}
}
