package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (w *world) env(pid int, dir string, vars map[string]string) (Env, []string) {
	vars["XDG_RUNTIME_DIR"] = filepath.Join(w.root, "run")
	vars["XDG_CONFIG_HOME"] = filepath.Join(w.root, "xdgconfig")
	var environ []string
	for k, v := range vars {
		environ = append(environ, k+"="+v)
	}
	return Env{
		LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok },
		Dir:       dir, Home: w.home, PID: pid,
	}, environ
}

func lookupIn(environ []string, k string) (string, bool) {
	for _, kv := range environ {
		if name, v, ok := strings.Cut(kv, "="); ok && name == k {
			return v, true
		}
	}
	return "", false
}

func TestPrepare(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-eu\n", true)
	e, environ := w.env(300, w.path("proj/sub"), map[string]string{"PATH": "/bin"})
	p, err := Prepare(e, environ)
	if err != nil {
		t.Fatal(err)
	}
	kc, _ := lookupIn(p.Environ, "KUBECONFIG")
	ov, _ := lookupIn(p.Environ, EnvOverlay)
	if !strings.HasPrefix(kc, ov+":") || !strings.HasSuffix(ov, "/300.yaml") {
		t.Fatalf("KUBECONFIG = %q, overlay %q", kc, ov)
	}
	if v, _ := lookupIn(p.Environ, "PATH"); v != "/bin" {
		t.Error("PATH not preserved")
	}
	if _, err := os.Stat(ov); err != nil {
		t.Fatal(err)
	}
	if msgs := p.Cleanup(); len(msgs) != 0 {
		t.Errorf("cleanup messages: %v", msgs)
	}
	if _, err := os.Stat(ov); !os.IsNotExist(err) {
		t.Fatal("overlay not cleaned up")
	}
}

func TestPrepareNotAllowedRefuses(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-eu\n", false)
	e, environ := w.env(300, w.path("proj"), map[string]string{})
	_, err := Prepare(e, environ)
	if !errors.Is(err, ErrNotAllowed) || !strings.Contains(err.Error(), "~/proj") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareNotGoverned(t *testing.T) {
	w := newWorld(t)
	e, environ := w.env(300, w.path("x"), map[string]string{"KUBECONFIG": "/a"})
	p, err := Prepare(e, environ)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := lookupIn(p.Environ, "KUBECONFIG"); v != "/a" {
		t.Fatalf("KUBECONFIG = %q", v)
	}
	p.Cleanup()
}

// kdir exec run from a hooked shell gets its own overlay and leaves the
// shell's alone.
func TestPrepareInsideHookedShell(t *testing.T) {
	w := newWorld(t)
	w.project("proj", "context: staging-eu\n", true)
	s := w.shell(100, nil)
	s.cd("proj")
	shellOverlay := s.env[EnvOverlay]

	vars := map[string]string{}
	for k, v := range s.env {
		vars[k] = v
	}
	e, environ := w.env(300, w.path("proj"), vars)
	p, err := Prepare(e, environ)
	if err != nil {
		t.Fatal(err)
	}
	ov, _ := lookupIn(p.Environ, EnvOverlay)
	if ov == shellOverlay {
		t.Fatal("exec reused the shell's overlay")
	}
	if orig, _ := lookupIn(p.Environ, EnvOrigSet); orig != "0" {
		t.Fatalf("original KUBECONFIG lost: %q", orig)
	}
	p.Cleanup()
	if _, err := os.Stat(shellOverlay); err != nil {
		t.Fatal("exec cleanup removed the shell's overlay")
	}

	// From outside the governed tree, exec restores the user's original.
	e, environ = w.env(301, w.path("elsewhere"), vars)
	p, err = Prepare(e, environ)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupIn(p.Environ, "KUBECONFIG"); ok {
		t.Fatal("KUBECONFIG should be unset outside the tree, as originally")
	}
	p.Cleanup()
	if _, err := os.Stat(shellOverlay); err != nil {
		t.Fatal("exec removed the shell's overlay")
	}
}
