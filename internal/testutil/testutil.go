// Package testutil holds helpers shared by kdir's tests.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Root returns the repository root.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// Fixture returns the absolute path of a file under testdata/.
func Fixture(name string) string {
	return filepath.Join(Root(), "testdata", name)
}

// CopyFixture copies testdata/<name> into dir (under the same base name) and
// returns the new path, so tests can modify it freely.
func CopyFixture(t testing.TB, name, dir string) string {
	t.Helper()
	data, err := os.ReadFile(Fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

// WriteFile writes data to dir/name, creating parent directories.
func WriteFile(t testing.TB, path, data string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// GenKubeconfig returns a kubeconfig with n clusters, users and contexts
// named ctx-0..ctx-(n-1), with fake credentials. current is the
// current-context.
func GenKubeconfig(n int, current string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\nkind: Config\ncurrent-context: %s\nclusters:\n", current)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- name: cluster-%d\n  cluster:\n    server: https://cluster-%d.example.com:6443\n    certificate-authority-data: ZmFrZS1jYQ==\n", i, i)
	}
	b.WriteString("users:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- name: user-%d\n  user:\n    token: fake-token-%d\n", i, i)
	}
	b.WriteString("contexts:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- name: ctx-%d\n  context:\n    cluster: cluster-%d\n    user: user-%d\n    namespace: ns-%d\n", i, i, i, i)
	}
	return b.String()
}

// Snapshot records the bytes and mtimes of files so a test can assert they
// were never modified.
type Snapshot map[string]snap

type snap struct {
	data  string
	mtime int64
}

// TakeSnapshot records the given files.
func TakeSnapshot(t testing.TB, files ...string) Snapshot {
	t.Helper()
	s := Snapshot{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		s[f] = snap{string(data), fi.ModTime().UnixNano()}
	}
	return s
}

// AssertUnchanged fails the test if any recorded file's bytes or mtime changed.
func (s Snapshot) AssertUnchanged(t testing.TB) {
	t.Helper()
	for f, want := range s {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		fi, err := os.Stat(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if string(data) != want.data {
			t.Errorf("%s: contents changed:\n%s", f, data)
		}
		if fi.ModTime().UnixNano() != want.mtime {
			t.Errorf("%s: mtime changed", f)
		}
	}
}
