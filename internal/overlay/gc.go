package overlay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// GCResult reports what GC did.
type GCResult struct {
	Removed []string // stale overlays deleted
	Saved   []string // stale overlays kept because they hold credentials (renamed kept-*)
	Kept    []string // kept-* files already present, for the user to merge or delete
}

// alive reports whether a process exists. EPERM means it exists but belongs
// to someone else, which still counts.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// GC deletes overlays in dir whose shell is no longer running, plus
// abandoned temp files. Overlays another tool wrote credentials into are
// renamed to kept-<pid>-<time>.yaml instead of being deleted.
func GC(dir string) (GCResult, error) {
	var res GCResult
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		switch {
		case strings.HasPrefix(name, "kept-"):
			res.Kept = append(res.Kept, path)
		case strings.HasPrefix(name, ".tmp-"):
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(path)
			}
		default:
			pid, ok := PIDFromPath(path)
			if !ok || alive(pid) {
				continue
			}
			if has, err := HasCredentials(path); err != nil || has {
				kept := filepath.Join(dir, KeptName(pid))
				if err := os.Rename(path, kept); err == nil {
					res.Saved = append(res.Saved, kept)
				}
				continue
			}
			if err := os.Remove(path); err == nil {
				res.Removed = append(res.Removed, path)
			}
		}
	}
	return res, nil
}

// KeptName is the file name an overlay with credentials is saved under.
func KeptName(pid int) string {
	return fmt.Sprintf("kept-%d-%d.yaml", pid, time.Now().UnixNano())
}

const gcStampName = ".gc-stamp"

// GCDue reports whether the daily opportunistic GC should run, and if so
// marks it as done. It costs one stat when not due.
func GCDue(dir string, now time.Time) bool {
	stamp := filepath.Join(dir, gcStampName)
	if fi, err := os.Stat(stamp); err == nil && now.Sub(fi.ModTime()) < 24*time.Hour {
		return false
	}
	f, err := os.OpenFile(stamp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Chtimes(stamp, now, now)
	return true
}
