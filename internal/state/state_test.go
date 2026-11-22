package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeDecode(t *testing.T) {
	tests := []State{
		{PID: 123, Mode: Active, Fingerprint: "abc", File: "/home/u/src/payments/.kubectx"},
		{PID: 1, Mode: FailClosed, Fingerprint: "x", File: "/weird|path with spaces/$HOME/'q'/.kubectx"},
		{PID: 99999, Mode: NotAllowed, Fingerprint: "", File: "/a/.kubectx"},
	}
	for _, want := range tests {
		got, ok := Decode(want.Encode())
		if !ok || got != want {
			t.Errorf("round trip %+v -> %q -> %+v, %v", want, want.Encode(), got, ok)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, v := range []string{
		"", "garbage", "v0|1|a|fp|/f", "v1|x|a|fp|/f", "v1|1|z|fp|/f", "v1|1|aa|fp|/f", "v1|1|a|fp",
	} {
		if _, ok := Decode(v); ok {
			t.Errorf("Decode(%q) accepted", v)
		}
	}
}

func TestFingerprint(t *testing.T) {
	dir := t.TempDir()
	kubectx := filepath.Join(dir, ".kubectx")
	entry := filepath.Join(dir, "entry")
	kc := filepath.Join(dir, "config")
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(kubectx, "context: a\n")
	write(kc, "apiVersion: v1\n")
	in := Inputs{KubectxFile: kubectx, TrustEntry: entry, KubeconfigFiles: []string{kc, filepath.Join(dir, "missing")}}
	base := Fingerprint(in)
	if base != Fingerprint(in) {
		t.Fatal("fingerprint not deterministic")
	}

	bump := func(p string) {
		t.Helper()
		future := time.Now().Add(time.Duration(len(p)) * time.Hour)
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}
	changes := []struct {
		name string
		do   func() Inputs
	}{
		{"kubectx edited", func() Inputs { write(kubectx, "context: b\n"); return in }},
		{"kubectx touched", func() Inputs { bump(kubectx); return in }},
		{"allowed", func() Inputs { write(entry, "x"); return in }},
		{"denied", func() Inputs { _ = os.Remove(entry); return in }},
		{"kubeconfig changed", func() Inputs { write(kc, "apiVersion: v1\nkind: Config\n"); return in }},
		{"missing kubeconfig appears", func() Inputs { write(filepath.Join(dir, "missing"), ""); return in }},
		{"orig KUBECONFIG changed", func() Inputs { c := in; c.OrigKubeconfig = "/other"; c.OrigSet = true; return c }},
		{"orig set vs unset", func() Inputs { c := in; c.OrigSet = true; return c }},
	}
	prev := base
	for _, c := range changes {
		got := Fingerprint(c.do())
		if got == prev {
			t.Errorf("%s: fingerprint did not change", c.name)
		}
		prev = got
	}
}
