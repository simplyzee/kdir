package overlay

// These tests pin down how kubectl's config machinery treats
// KUBECONFIG=<overlay>:<real files>, instead of assuming it. They use the same
// clientcmd calls kubectl makes (GetStartingConfig + ModifyConfig), and, when
// kubectl is installed, the real kubectl binary too.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/thomaschaplin/kdir/internal/kubeconfig"
	"github.com/thomaschaplin/kdir/internal/testutil"
)

type fixture struct {
	overlay string
	real    []string
	snap    testutil.Snapshot
	env     string // KUBECONFIG value
}

// newFixture writes an overlay selecting current/namespace in front of copies
// of the given real kubeconfig fixtures. With targetOnly the overlay holds
// only the target context, as the original spec proposed.
func newFixture(t *testing.T, current, namespace string, targetOnly bool, realFixtures ...string) fixture {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for _, name := range realFixtures {
		files = append(files, testutil.CopyFixture(t, "kubeconfig/"+name, dir))
	}
	cfg, err := kubeconfig.Load(files)
	if err != nil {
		t.Fatal(err)
	}
	f := Build(cfg, current, namespace)
	if targetOnly {
		var only []NamedContext
		for _, c := range f.Contexts {
			if c.Name == current {
				only = append(only, c)
			}
		}
		f.Contexts = only
	}
	overlay := filepath.Join(dir, "overlay.yaml")
	if err := Write(overlay, f); err != nil {
		t.Fatal(err)
	}
	env := strings.Join(append([]string{overlay}, files...), string(os.PathListSeparator))
	return fixture{overlay: overlay, real: files, snap: testutil.TakeSnapshot(t, files...), env: env}
}

// modify runs fn against the starting config exactly like `kubectl config ...`.
func (fx fixture) modify(t *testing.T, fn func(*clientcmdapi.Config)) {
	t.Helper()
	t.Setenv("KUBECONFIG", fx.env)
	po := clientcmd.NewDefaultPathOptions()
	cfg, err := po.GetStartingConfig()
	if err != nil {
		t.Fatal(err)
	}
	fn(cfg)
	if err := clientcmd.ModifyConfig(po, *cfg, true); err != nil {
		t.Fatal(err)
	}
}

func (fx fixture) clientConfig() clientcmd.ClientConfig {
	rules := &clientcmd.ClientConfigLoadingRules{Precedence: filepath.SplitList(fx.env)}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
}

func TestMergedViewUsesOverlayContextAndRealCredentials(t *testing.T) {
	fx := newFixture(t, "staging-eu", "payments", false, "basic.yaml")
	cc := fx.clientConfig()
	raw, err := cc.RawConfig()
	if err != nil {
		t.Fatal(err)
	}
	if raw.CurrentContext != "staging-eu" {
		t.Errorf("current-context = %q, want staging-eu (real file says prod-eu)", raw.CurrentContext)
	}
	ns, _, err := cc.Namespace()
	if err != nil || ns != "payments" {
		t.Errorf("namespace = %q, %v; want payments (real file says default)", ns, err)
	}
	rest, err := cc.ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	if rest.Host != "https://ABCD1234.gr7.eu-west-1.eks.amazonaws.com" || rest.BearerToken != "fake-token-staging-admin" {
		t.Errorf("cluster/user not taken from real file: host %q token %q", rest.Host, rest.BearerToken)
	}
	fx.snap.AssertUnchanged(t)
}

func TestMergedViewMultiFileOriginal(t *testing.T) {
	fx := newFixture(t, "beta", "web", false, "multi-a.yaml", "multi-b.yaml")
	cc := fx.clientConfig()
	raw, _ := cc.RawConfig()
	ns, _, _ := cc.Namespace()
	if raw.CurrentContext != "beta" || ns != "web" {
		t.Errorf("got %s/%s, want beta/web", raw.CurrentContext, ns)
	}
	// The overlay copied "alpha" from the winning file (multi-a.yaml), so the
	// shadowing order of the original list is preserved.
	if got := raw.Contexts["alpha"]; got.Namespace != "alpha-ns" || got.Cluster != "alpha" {
		t.Errorf("alpha = %+v, want multi-a.yaml's entry", got)
	}
}

func TestFailClosedOverlayBreaksKubectl(t *testing.T) {
	cfg, _ := kubeconfig.Load([]string{testutil.Fixture("kubeconfig/basic.yaml")})
	fx := newFixture(t, FailClosedName(cfg, "missing", "staging-us"), "", false, "basic.yaml")
	if _, err := fx.clientConfig().ClientConfig(); err == nil {
		t.Fatal("fail-closed overlay produced a usable client config; it would hit prod-eu")
	}
}

func TestUseContextWritesOnlyOverlay(t *testing.T) {
	fx := newFixture(t, "staging-eu", "payments", false, "basic.yaml")
	fx.modify(t, func(c *clientcmdapi.Config) { c.CurrentContext = "dev" })
	fx.snap.AssertUnchanged(t)
	ov, err := clientcmd.LoadFromFile(fx.overlay)
	if err != nil {
		t.Fatal(err)
	}
	if ov.CurrentContext != "dev" {
		t.Errorf("overlay current-context = %q, want dev", ov.CurrentContext)
	}
}

func TestNamespaceChangeOnTargetWritesOnlyOverlay(t *testing.T) {
	for _, targetOnly := range []bool{false, true} {
		fx := newFixture(t, "staging-eu", "payments", targetOnly, "basic.yaml")
		fx.modify(t, func(c *clientcmdapi.Config) { c.Contexts["staging-eu"].Namespace = "web" })
		fx.snap.AssertUnchanged(t)
	}
}

// After switching to another context inside the shell, a namespace change
// goes to the file that context came from. With every context copied into the
// overlay, that is the overlay.
func TestNamespaceChangeOnOtherContextWritesOnlyOverlay(t *testing.T) {
	fx := newFixture(t, "staging-eu", "payments", false, "basic.yaml")
	fx.modify(t, func(c *clientcmdapi.Config) { c.CurrentContext = "dev" })
	fx.modify(t, func(c *clientcmdapi.Config) { c.Contexts["dev"].Namespace = "web" })
	fx.snap.AssertUnchanged(t)
	ns, _, _ := fx.clientConfig().Namespace()
	if ns != "web" {
		t.Errorf("namespace = %q, want web", ns)
	}
}

// Documents why the overlay copies every context: with only the target
// context, the same sequence writes to the user's real kubeconfig.
func TestTargetOnlyOverlayWouldLeakNamespaceChange(t *testing.T) {
	fx := newFixture(t, "staging-eu", "payments", true, "basic.yaml")
	fx.modify(t, func(c *clientcmdapi.Config) { c.CurrentContext = "dev" })
	fx.modify(t, func(c *clientcmdapi.Config) { c.Contexts["dev"].Namespace = "web" })
	data, _ := os.ReadFile(fx.real[0])
	if !strings.Contains(string(data), "namespace: web") {
		t.Fatal("expected target-only overlay to leak the namespace change into the real file; clientcmd behaviour changed")
	}
}

// Tools that add credentials (aws eks update-kubeconfig, kubectl config
// set-credentials) write to the first KUBECONFIG file: the overlay.
func TestNewCredentialsLandInOverlay(t *testing.T) {
	fx := newFixture(t, "staging-eu", "payments", false, "basic.yaml")
	fx.modify(t, func(c *clientcmdapi.Config) {
		c.AuthInfos["new-user"] = &clientcmdapi.AuthInfo{Token: "fake-new"}
		c.Clusters["new-cluster"] = &clientcmdapi.Cluster{Server: "https://new.example.com"}
	})
	fx.snap.AssertUnchanged(t)
	if has, err := HasCredentials(fx.overlay); err != nil || !has {
		t.Fatalf("HasCredentials = %v, %v; want true", has, err)
	}
	if err := Remove(fx.overlay); !errors.Is(err, ErrHasCredentials) {
		t.Fatalf("Remove = %v, want ErrHasCredentials", err)
	}
}

func TestRealKubectl(t *testing.T) {
	kubectl, err := exec.LookPath("kubectl")
	if err != nil {
		t.Skip("kubectl not installed")
	}
	fx := newFixture(t, "staging-eu", "payments", false, "basic.yaml")
	home := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(kubectl, args...)
		cmd.Env = []string{"KUBECONFIG=" + fx.env, "HOME=" + home, "PATH=" + os.Getenv("PATH")}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("kubectl %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	nsOf := func() string { return run("config", "view", "--minify", "-o", "jsonpath={..namespace}") }

	if got := run("config", "current-context"); got != "staging-eu" {
		t.Errorf("current-context = %q", got)
	}
	if got := nsOf(); got != "payments" {
		t.Errorf("namespace = %q", got)
	}
	run("config", "set-context", "--current", "--namespace=web")
	run("config", "use-context", "dev")
	run("config", "set-context", "--current", "--namespace=tools")
	if got := run("config", "current-context"); got != "dev" {
		t.Errorf("after use-context: %q", got)
	}
	if got := nsOf(); got != "tools" {
		t.Errorf("namespace after set-context = %q", got)
	}
	run("config", "set-credentials", "kdir-test-user", "--token=fake")
	fx.snap.AssertUnchanged(t)
	if has, _ := HasCredentials(fx.overlay); !has {
		t.Error("set-credentials did not land in the overlay")
	}
}

// Documents why the prompt hook must check that the overlay still exists:
// clientcmd skips missing KUBECONFIG entries, so a deleted overlay (for
// example one cleaned out of $TMPDIR) silently falls back to the real
// current-context.
func TestMissingOverlayFailsOpen(t *testing.T) {
	cfg, _ := kubeconfig.Load([]string{testutil.Fixture("kubeconfig/basic.yaml")})
	fx := newFixture(t, FailClosedName(cfg, "missing", "x"), "", false, "basic.yaml")
	if err := os.Remove(fx.overlay); err != nil {
		t.Fatal(err)
	}
	raw, err := fx.clientConfig().RawConfig()
	if err != nil {
		t.Fatal(err)
	}
	if raw.CurrentContext != "prod-eu" {
		t.Fatalf("current-context = %q; expected the fall-back to prod-eu this test documents", raw.CurrentContext)
	}
}
