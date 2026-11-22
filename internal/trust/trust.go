// Package trust records which .kubectx files the user has allowed, in the
// spirit of `direnv allow`.
//
// Each allowed file has one entry in the store directory, named by
// sha256(absolute path). The entry holds sha256(path + NUL + contents), so a
// file that is moved or edited is no longer allowed, while `deny` and `status`
// can still find the entry for an edited file.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Status of a .kubectx file in the trust store.
type Status int

const (
	// NotAllowed means the file has never been allowed (or was denied).
	NotAllowed Status = iota
	// Allowed means the file's path and contents match the allowed entry.
	Allowed
	// Changed means the file was allowed but its contents changed since.
	Changed
)

func (s Status) String() string {
	switch s {
	case Allowed:
		return "allowed"
	case Changed:
		return "changed since allowed"
	}
	return "not allowed"
}

// Store is a directory of allow entries.
type Store struct{ Dir string }

// DefaultDir returns $XDG_CONFIG_HOME/kdir/allowed, defaulting to
// ~/.config/kdir/allowed.
func DefaultDir(getenv func(string) string, home string) string {
	base := getenv("XDG_CONFIG_HOME")
	if base == "" || !filepath.IsAbs(base) {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "kdir", "allowed")
}

// EntryPath returns the entry file for a .kubectx path. The fast path stats
// it to notice `kdir allow` / `kdir deny` without reading anything.
func (s Store) EntryPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:]))
}

func digest(path string, contents []byte) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(contents)
	return hex.EncodeToString(h.Sum(nil))
}

func checkAbs(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("trust: path %q must be absolute and clean", path)
	}
	return nil
}

// Allow records path with its current contents.
func (s Store) Allow(path string, contents []byte) error {
	if err := checkAbs(path); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	data := digest(path, contents) + "\n" + path + "\n"
	tmp, err := os.CreateTemp(s.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.EntryPath(path))
}

// Deny removes the entry for path. Denying a file that is not allowed is not
// an error.
func (s Store) Deny(path string) error {
	if err := checkAbs(path); err != nil {
		return err
	}
	if err := os.Remove(s.EntryPath(path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Check reports whether path with the given contents is allowed.
func (s Store) Check(path string, contents []byte) (Status, error) {
	if err := checkAbs(path); err != nil {
		return NotAllowed, err
	}
	data, err := os.ReadFile(s.EntryPath(path))
	if errors.Is(err, fs.ErrNotExist) {
		return NotAllowed, nil
	}
	if err != nil {
		return NotAllowed, err
	}
	want, _, _ := strings.Cut(string(data), "\n")
	if want == digest(path, contents) {
		return Allowed, nil
	}
	return Changed, nil
}
