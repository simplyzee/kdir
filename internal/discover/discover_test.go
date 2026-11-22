package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFind(t *testing.T) {
	// Layout (root is a temp dir standing in for "/"):
	//   root/.kubectx                       (above home: ignored when home is a stop)
	//   root/home/.kubectx                  (user-wide default)
	//   root/home/src/payments/.kubectx
	//   root/home/src/payments/svc/api/     (nested, no file)
	//   root/home/src/payments/svc/web/.kubectx (nearer file wins)
	//   root/home/src/dirfile/.kubectx/     (a directory, not a file)
	//   root/opt/work/                      (outside home)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	touch := func(p string) {
		mk(filepath.Dir(p))
		if err := os.WriteFile(filepath.Join(root, p), []byte("context: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch(".kubectx")
	touch("home/.kubectx")
	touch("home/src/payments/.kubectx")
	mk("home/src/payments/svc/api")
	touch("home/src/payments/svc/web/.kubectx")
	mk("home/src/dirfile/.kubectx")
	mk("opt/work")
	mk("nohome/home2/x")

	tests := []struct {
		name  string
		start string
		stops []string
		want  string // relative to root, "" for not found
	}{
		{"file in start dir", "home/src/payments", []string{home}, "home/src/payments/.kubectx"},
		{"nested walks up", "home/src/payments/svc/api", []string{home}, "home/src/payments/.kubectx"},
		{"nearest wins", "home/src/payments/svc/web", []string{home}, "home/src/payments/svc/web/.kubectx"},
		{"home default", "home/src", []string{home}, "home/.kubectx"},
		{"home itself", "home", []string{home}, "home/.kubectx"},
		{"directory named .kubectx ignored", "home/src/dirfile", []string{home}, "home/.kubectx"},
		{"outside home walks to root", "opt/work", []string{home}, ".kubectx"},
		{"no stops walks to root", "home/src", nil, "home/.kubectx"},
		{"stop below file prevents finding it", "nohome/home2/x", []string{filepath.Join(root, "nohome/home2")}, ""},
		{"trailing slash and dots cleaned", "home/src/payments/svc/../svc/api/", []string{home + "/"}, "home/src/payments/.kubectx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Find(filepath.Join(root, tt.start), tt.stops...)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("got %s, want nothing", got.Path)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nothing, want %s", tt.want)
			}
			want := filepath.Join(root, tt.want)
			if got.Path != want || got.Dir != filepath.Dir(want) {
				t.Fatalf("got %s (dir %s), want %s", got.Path, got.Dir, want)
			}
			if got.Size != int64(len("context: x\n")) || got.ModTime.IsZero() {
				t.Fatalf("bad stat info: %+v", got)
			}
		})
	}
}

func TestFindNoFileAnywhere(t *testing.T) {
	root := t.TempDir()
	start := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Find(start, root)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

func TestFindStartDirRemoved(t *testing.T) {
	// The shell's $PWD can be deleted out from under it; that must not error.
	root := t.TempDir()
	got, err := Find(filepath.Join(root, "gone", "deeper"), root)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

func TestFindParentIsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Find(filepath.Join(root, "f", "sub"), root)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}
