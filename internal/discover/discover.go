// Package discover finds the .kubectx file that governs a directory.
package discover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// FileName is the name of the per-directory spec file.
const FileName = ".kubectx"

// Found describes a governing .kubectx file.
type Found struct {
	Path    string // absolute path of the .kubectx file
	Dir     string // directory containing it (exported as KDIR_ROOT)
	ModTime time.Time
	Size    int64
}

// Find walks up from start (an absolute directory, normally the logical $PWD)
// and returns the nearest regular .kubectx file, or nil if there is none.
//
// The walk normally continues to "/". If it reaches one of the stop
// directories (normally $HOME and its symlink-resolved form), that directory
// is still checked, so ~/.kubectx acts as a user-wide default, but nothing
// above it is. Directories outside $HOME are walked all the way to "/".
//
// Find only stats files; it never reads or parses them, so it is cheap enough
// to run on every prompt.
func Find(start string, stops ...string) (*Found, error) {
	dir := filepath.Clean(start)
	stopSet := make(map[string]bool, len(stops))
	for _, s := range stops {
		if s != "" {
			stopSet[filepath.Clean(s)] = true
		}
	}
	for {
		p := filepath.Join(dir, FileName)
		fi, err := os.Stat(p)
		switch {
		case err == nil && fi.Mode().IsRegular():
			return &Found{Path: p, Dir: dir, ModTime: fi.ModTime(), Size: fi.Size()}, nil
		case err != nil && !isNotExist(err):
			return nil, err
		}
		if stopSet[dir] {
			return nil, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

// isNotExist treats "a path component is not a directory" the same as
// "does not exist", which happens if a parent was replaced by a file.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}
