package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllowCheckDeny(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "allowed")}
	path := "/home/u/src/payments/.kubectx"
	contents := []byte("context: staging-eu\n")

	check := func(p string, c []byte, want Status) {
		t.Helper()
		got, err := s.Check(p, c)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("Check(%s) = %v, want %v", p, got, want)
		}
	}

	check(path, contents, NotAllowed)
	if err := s.Allow(path, contents); err != nil {
		t.Fatal(err)
	}
	check(path, contents, Allowed)
	check(path, []byte("context: prod-eu\n"), Changed)
	check("/home/u/src/other/.kubectx", contents, NotAllowed) // same contents, different path

	// Re-allowing after an edit accepts the new contents only.
	edited := []byte("context: staging-eu\nnamespace: web\n")
	if err := s.Allow(path, edited); err != nil {
		t.Fatal(err)
	}
	check(path, edited, Allowed)
	check(path, contents, Changed)

	// Deny works on an edited file and is idempotent.
	if err := s.Deny(path); err != nil {
		t.Fatal(err)
	}
	check(path, edited, NotAllowed)
	if err := s.Deny(path); err != nil {
		t.Fatal(err)
	}
}

func TestStorePermissions(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "kdir", "allowed")}
	if err := s.Allow("/a/.kubectx", nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", fi.Mode().Perm())
	}
	fi, err = os.Stat(s.EntryPath("/a/.kubectx"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("entry mode %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 1 {
		t.Errorf("leftover temp files: %v", entries)
	}
}

func TestEntryPathStable(t *testing.T) {
	s := Store{Dir: "/x"}
	a1, a2, b := s.EntryPath("/a/.kubectx"), s.EntryPath("/a/.kubectx"), s.EntryPath("/b/.kubectx")
	if a1 != a2 || a1 == b {
		t.Fatal("entry paths not stable or not distinct")
	}
}

func TestRejectsRelativePaths(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, p := range []string{"rel/.kubectx", "/a/../b/.kubectx", ""} {
		if err := s.Allow(p, nil); err == nil {
			t.Errorf("Allow(%q) accepted", p)
		}
		if _, err := s.Check(p, nil); err == nil {
			t.Errorf("Check(%q) accepted", p)
		}
		if err := s.Deny(p); err == nil {
			t.Errorf("Deny(%q) accepted", p)
		}
	}
}

func TestDefaultDir(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := DefaultDir(get(nil), "/home/u"); got != "/home/u/.config/kdir/allowed" {
		t.Errorf("default = %s", got)
	}
	if got := DefaultDir(get(map[string]string{"XDG_CONFIG_HOME": "/xdg"}), "/home/u"); got != "/xdg/kdir/allowed" {
		t.Errorf("xdg = %s", got)
	}
	if got := DefaultDir(get(map[string]string{"XDG_CONFIG_HOME": "rel"}), "/home/u"); got != "/home/u/.config/kdir/allowed" {
		t.Errorf("relative xdg = %s", got)
	}
}
