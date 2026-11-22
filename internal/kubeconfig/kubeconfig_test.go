package kubeconfig

import (
	"reflect"
	"testing"

	"github.com/thomaschaplin/kdir/internal/testutil"
)

func TestFiles(t *testing.T) {
	tests := []struct {
		env  string
		want []string
	}{
		{"", []string{"/home/u/.kube/config"}},
		{"/a", []string{"/a"}},
		{"/a:/b", []string{"/a", "/b"}},
		{"/a::/b:", []string{"/a", "/b"}},
		{"/a:/b:/a", []string{"/a", "/b"}},
		{":", []string{"/home/u/.kube/config"}},
		{"/with space/config:/b", []string{"/with space/config", "/b"}},
	}
	for _, tt := range tests {
		if got := Files(tt.env, "/home/u"); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Files(%q) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestLoadMergesFirstFileWins(t *testing.T) {
	a := testutil.Fixture("kubeconfig/multi-a.yaml")
	b := testutil.Fixture("kubeconfig/multi-b.yaml")
	cfg, err := Load([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "alpha" {
		t.Errorf("current-context = %q, want alpha", cfg.CurrentContext)
	}
	if got := cfg.Contexts["alpha"]; got.Namespace != "alpha-ns" || got.Cluster != "alpha" {
		t.Errorf("alpha context = %+v, want the one from multi-a.yaml", got)
	}
	if _, ok := cfg.Contexts["beta"]; !ok {
		t.Error("beta context from second file missing")
	}

	cfg, err = Load([]string{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "beta" || cfg.Contexts["alpha"].Namespace != "shadowed" {
		t.Errorf("reversed order: got current %q, alpha ns %q", cfg.CurrentContext, cfg.Contexts["alpha"].Namespace)
	}
}

func TestLoadIgnoresMissingFiles(t *testing.T) {
	cfg, err := Load([]string{"/definitely/not/here", testutil.Fixture("kubeconfig/basic.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Contexts) != 5 {
		t.Fatalf("got %d contexts, want 5", len(cfg.Contexts))
	}
}

func TestLoadInvalidFile(t *testing.T) {
	p := testutil.WriteFile(t, t.TempDir()+"/bad", "contexts: {{{")
	if _, err := Load([]string{p}); err == nil {
		t.Fatal("expected error for invalid kubeconfig")
	}
}
