package overlay

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/thomaschaplin/kdir/internal/testutil"
)

// deadPID is far above any real pid_max, so no process has it.
const deadPID = 99999999

func TestGC(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		return testutil.WriteFile(t, filepath.Join(dir, name), content)
	}
	plain := "apiVersion: v1\nkind: Config\ncurrent-context: x\n"
	dead := write(filepath.Base(Path("", deadPID)), plain)
	live := write(filepath.Base(Path("", os.Getpid())), plain)
	creds := write(filepath.Base(Path("", deadPID-1)), plain+"users:\n- name: u\n  user: {token: fake}\n")
	oldTmp := write(".tmp-old", "")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldTmp, old, old); err != nil {
		t.Fatal(err)
	}
	newTmp := write(".tmp-new", "")
	kept := write("kept-1-2.yaml", plain)
	other := write("notes.txt", "")

	res, err := GC(dir)
	if err != nil {
		t.Fatal(err)
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(dead) || exists(oldTmp) || exists(creds) {
		t.Error("stale files not removed")
	}
	for _, p := range []string{live, newTmp, kept, other} {
		if !exists(p) {
			t.Errorf("%s removed", filepath.Base(p))
		}
	}
	if len(res.Removed) != 1 || res.Removed[0] != dead {
		t.Errorf("Removed = %v", res.Removed)
	}
	if len(res.Saved) != 1 || !strings.HasPrefix(filepath.Base(res.Saved[0]), "kept-99999998-") || !exists(res.Saved[0]) {
		t.Errorf("Saved = %v", res.Saved)
	}
	sort.Strings(res.Kept)
	if len(res.Kept) != 1 || res.Kept[0] != kept {
		t.Errorf("Kept = %v", res.Kept)
	}
}

func TestGCMissingDir(t *testing.T) {
	if _, err := GC(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
}

func TestGCDue(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if !GCDue(dir, now) {
		t.Fatal("first run should be due")
	}
	if GCDue(dir, now.Add(time.Hour)) {
		t.Fatal("due again within a day")
	}
	if !GCDue(dir, now.Add(25*time.Hour)) {
		t.Fatal("not due after a day")
	}
}
