package overlay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/thomaschaplin/kdir/internal/kubeconfig"
	"github.com/thomaschaplin/kdir/internal/testutil"
)

func loadBasic(t *testing.T) *clientcmdapi.Config {
	t.Helper()
	cfg, err := kubeconfig.Load([]string{testutil.Fixture("kubeconfig/basic.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuild(t *testing.T) {
	cfg := loadBasic(t)
	f := Build(cfg, "staging-eu", "payments")
	if f.CurrentContext != "staging-eu" || f.APIVersion != "v1" || f.Kind != "Config" {
		t.Fatalf("bad header: %+v", f)
	}
	if len(f.Contexts) != len(cfg.Contexts) {
		t.Fatalf("got %d contexts, want all %d", len(f.Contexts), len(cfg.Contexts))
	}
	byName := map[string]ContextRef{}
	for _, c := range f.Contexts {
		byName[c.Name] = c.Context
	}
	if got := byName["staging-eu"]; got != (ContextRef{Cluster: "staging-eu", User: "staging-admin", Namespace: "payments"}) {
		t.Errorf("target context = %+v", got)
	}
	// Other contexts are copied as-is, keeping their own namespace.
	if got := byName["prod-eu"]; got != (ContextRef{Cluster: "prod-eu", User: "prod-admin", Namespace: "payments"}) {
		t.Errorf("prod-eu = %+v", got)
	}
	if got := byName["dev"]; got.Namespace != "" {
		t.Errorf("dev namespace = %q, want empty", got.Namespace)
	}
	// Deterministic order.
	for i := 1; i < len(f.Contexts); i++ {
		if f.Contexts[i-1].Name > f.Contexts[i].Name {
			t.Fatal("contexts not sorted")
		}
	}
}

func TestBuildNilConfig(t *testing.T) {
	f := Build(nil, "kdir-invalid", "")
	if f.CurrentContext != "kdir-invalid" || len(f.Contexts) != 0 {
		t.Fatalf("got %+v", f)
	}
}

// The overlay must never contain clusters, users or any credential material.
func TestOverlayContainsNoCredentials(t *testing.T) {
	cfg := loadBasic(t)
	for _, current := range []string{"staging-eu", "kdir-missing-x"} {
		f := Build(cfg, current, "payments")
		data, err := f.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, forbidden := range []string{
			"clusters", "users", "server", "token", "password", "client-key", "client-certificate",
			"certificate-authority", "fake-", "ZmFrZS", "exec", "auth-provider",
		} {
			if strings.Contains(text, forbidden) {
				t.Errorf("overlay for %s contains %q:\n%s", current, forbidden, text)
			}
		}
		path := filepath.Join(t.TempDir(), "o.yaml")
		if err := Write(path, f); err != nil {
			t.Fatal(err)
		}
		loaded, err := clientcmd.LoadFromFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.Clusters) != 0 || len(loaded.AuthInfos) != 0 {
			t.Errorf("overlay parsed with %d clusters, %d users", len(loaded.Clusters), len(loaded.AuthInfos))
		}
		if has, err := HasCredentials(path); err != nil || has {
			t.Errorf("HasCredentials = %v, %v", has, err)
		}
	}
}

func TestWriteModeAndAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "123.yaml")
	if err := Write(path, Build(loadBasic(t), "dev", "")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	// Overwrite works and leaves no temp files behind.
	if err := Write(path, Build(nil, "x", "")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestFailClosedName(t *testing.T) {
	cfg := loadBasic(t)
	tests := []struct{ reason, want, expect string }{
		{"missing", "staging-us", "kdir-missing-staging-us"},
		{"missing", "arn:aws:eks:eu-west-1:1:cluster/x", "kdir-missing-arn-aws-eks-eu-west-1-1-cluster-x"},
		{"ambiguous", "", "kdir-ambiguous"},
		{"invalid", "!!!", "kdir-invalid"},
	}
	for _, tt := range tests {
		if got := FailClosedName(cfg, tt.reason, tt.want); got != tt.expect {
			t.Errorf("FailClosedName(%q, %q) = %q, want %q", tt.reason, tt.want, got, tt.expect)
		}
	}
	long := FailClosedName(nil, "missing", strings.Repeat("x", 200))
	if len(long) > 80 {
		t.Errorf("name too long: %d", len(long))
	}
	// Never collides with a real context.
	cfg.Contexts["kdir-missing-a"] = clientcmdapi.NewContext()
	cfg.Contexts["kdir-missing-a-2"] = clientcmdapi.NewContext()
	if got := FailClosedName(cfg, "missing", "a"); got != "kdir-missing-a-3" {
		t.Errorf("collision handling: got %q", got)
	}
}

func TestDir(t *testing.T) {
	t.Run("xdg runtime dir", func(t *testing.T) {
		xdg := t.TempDir()
		dir, err := Dir(env(map[string]string{"XDG_RUNTIME_DIR": xdg}))
		if err != nil {
			t.Fatal(err)
		}
		if dir != filepath.Join(xdg, "kdir") {
			t.Errorf("dir = %s", dir)
		}
		assertMode(t, dir, 0o700)
	})
	t.Run("tmpdir fallback", func(t *testing.T) {
		tmp := t.TempDir()
		dir, err := Dir(env(map[string]string{"TMPDIR": tmp}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(filepath.Base(dir), "kdir-") || filepath.Dir(dir) != tmp {
			t.Errorf("dir = %s", dir)
		}
		assertMode(t, dir, 0o700)
	})
	t.Run("relative xdg ignored", func(t *testing.T) {
		tmp := t.TempDir()
		dir, err := Dir(env(map[string]string{"XDG_RUNTIME_DIR": "relative", "TMPDIR": tmp}))
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(dir) != tmp {
			t.Errorf("dir = %s", dir)
		}
	})
	t.Run("loose permissions tightened", func(t *testing.T) {
		xdg := t.TempDir()
		if err := os.Mkdir(filepath.Join(xdg, "kdir"), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(xdg, "kdir"), 0o777); err != nil {
			t.Fatal(err)
		}
		dir, err := Dir(env(map[string]string{"XDG_RUNTIME_DIR": xdg}))
		if err != nil {
			t.Fatal(err)
		}
		assertMode(t, dir, 0o700)
	})
	t.Run("symlink refused", func(t *testing.T) {
		xdg := t.TempDir()
		if err := os.Symlink(t.TempDir(), filepath.Join(xdg, "kdir")); err != nil {
			t.Fatal(err)
		}
		if _, err := Dir(env(map[string]string{"XDG_RUNTIME_DIR": xdg})); err == nil {
			t.Fatal("expected symlinked overlay dir to be refused")
		}
	})
}

func TestPath(t *testing.T) {
	p := Path("/run/kdir", 4242)
	if p != "/run/kdir/4242.yaml" {
		t.Fatalf("Path = %s", p)
	}
	if pid, ok := PIDFromPath(p); !ok || pid != 4242 {
		t.Fatalf("PIDFromPath = %d, %v", pid, ok)
	}
	for _, bad := range []string{"/x/.tmp-1", "/x/abc.yaml", "/x/0.yaml", "/x/-1.yaml", "/x/12.yml"} {
		if _, ok := PIDFromPath(bad); ok {
			t.Errorf("PIDFromPath(%q) accepted", bad)
		}
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.yaml")
	if err := Write(path, Build(nil, "x", "")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("overlay not removed")
	}
	if err := Remove(path); err != nil {
		t.Fatalf("removing missing overlay: %v", err)
	}

	// An overlay another tool wrote credentials into is kept.
	testutil.WriteFile(t, path, `apiVersion: v1
kind: Config
current-context: x
users:
- name: new
  user: {token: fake}
`)
	if err := Remove(path); !errors.Is(err, ErrHasCredentials) {
		t.Fatalf("Remove = %v, want ErrHasCredentials", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("overlay with credentials was deleted")
	}
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != want {
		t.Errorf("%s mode = %v, want %v", path, fi.Mode().Perm(), want)
	}
}
