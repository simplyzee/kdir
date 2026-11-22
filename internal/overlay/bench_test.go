package overlay

import (
	"path/filepath"
	"testing"

	"github.com/thomaschaplin/kdir/internal/kubeconfig"
	"github.com/thomaschaplin/kdir/internal/resolve"
	"github.com/thomaschaplin/kdir/internal/spec"
	"github.com/thomaschaplin/kdir/internal/state"
	"github.com/thomaschaplin/kdir/internal/testutil"
)

// BenchmarkSwitch measures the in-process work of a directory switch on a
// 50-context kubeconfig: load, parse, resolve, build and write the overlay.
func BenchmarkSwitch(b *testing.B) {
	dir := b.TempDir()
	kc := testutil.WriteFile(b, filepath.Join(dir, "config"), testutil.GenKubeconfig(50, "ctx-0"))
	data := []byte("server: https://cluster-42.example.com:6443\nnamespace: payments\n")
	out := filepath.Join(dir, "1.yaml")
	for b.Loop() {
		cfg, err := kubeconfig.Load([]string{kc})
		if err != nil {
			b.Fatal(err)
		}
		s, _, err := spec.Parse(data)
		if err != nil {
			b.Fatal(err)
		}
		r := resolve.Resolve(s, cfg)
		if err := Write(out, Build(cfg, r.Context, r.Namespace)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFingerprint measures the no-change fast path's per-prompt work.
func BenchmarkFingerprint(b *testing.B) {
	dir := b.TempDir()
	in := state.Inputs{
		KubectxFile:     testutil.WriteFile(b, filepath.Join(dir, ".kubectx"), "context: a\n"),
		TrustEntry:      filepath.Join(dir, "entry"),
		KubeconfigFiles: []string{testutil.WriteFile(b, filepath.Join(dir, "config"), testutil.GenKubeconfig(50, "ctx-0"))},
	}
	for b.Loop() {
		_ = state.Fingerprint(in)
	}
}
