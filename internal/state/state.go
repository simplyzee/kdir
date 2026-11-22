// Package state encodes KDIR_STATE, which lets the prompt hook decide that
// nothing changed using only a handful of stat calls: no YAML parsing and no
// kubeconfig loading.
//
// The state records which .kubectx governs the shell, what kdir decided about
// it, and a fingerprint of every input that could change that decision: the
// .kubectx file, its trust entry, the user's real kubeconfig files and the
// original KUBECONFIG value. If the fingerprint recomputed at the next prompt
// matches, the hook prints nothing.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Version prefixes encoded states so a newer kdir can ignore an older format.
const Version = "v1"

// Mode is what kdir decided for the governing file.
type Mode byte

const (
	// Active means the target context is selected.
	Active Mode = 'a'
	// FailClosed means kubectl is pointed at a non-existent context.
	FailClosed Mode = 'f'
	// NotAllowed means the file is not trusted, so nothing was switched.
	NotAllowed Mode = 'n'
)

// State is the decoded value of KDIR_STATE.
type State struct {
	PID         int    // shell the state belongs to
	Mode        Mode   // decision for File
	Fingerprint string // see Fingerprint
	File        string // governing .kubectx path
}

// Encode renders the state. The file path goes last so it may contain "|".
func (s State) Encode() string {
	return fmt.Sprintf("%s|%d|%c|%s|%s", Version, s.PID, s.Mode, s.Fingerprint, s.File)
}

// Decode parses KDIR_STATE. ok is false for empty or unrecognised values,
// which callers treat as "no state".
func Decode(v string) (State, bool) {
	parts := strings.SplitN(v, "|", 5)
	if len(parts) != 5 || parts[0] != Version || len(parts[2]) != 1 {
		return State{}, false
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		return State{}, false
	}
	m := Mode(parts[2][0])
	if m != Active && m != FailClosed && m != NotAllowed {
		return State{}, false
	}
	return State{PID: pid, Mode: m, Fingerprint: parts[3], File: parts[4]}, true
}

// Inputs are the things a kdir decision depends on.
type Inputs struct {
	KubectxFile     string   // governing .kubectx
	TrustEntry      string   // its trust store entry
	KubeconfigFiles []string // the user's real kubeconfig files, in order
	OrigKubeconfig  string   // original KUBECONFIG value
	OrigSet         bool     // whether KUBECONFIG was set originally
}

// Fingerprint hashes the mtime and size of every input file (or its absence)
// together with the original KUBECONFIG value. It only calls stat.
func Fingerprint(in Inputs) string {
	h := sha256.New()
	stat := func(p string) {
		fi, err := os.Stat(p)
		if err != nil {
			_, _ = fmt.Fprintf(h, "%s\x00-\x00", p)
			return
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\x00", p, fi.ModTime().UnixNano(), fi.Size())
	}
	stat(in.KubectxFile)
	stat(in.TrustEntry)
	for _, f := range in.KubeconfigFiles {
		stat(f)
	}
	_, _ = fmt.Fprintf(h, "%t\x00%s", in.OrigSet, in.OrigKubeconfig)
	return hex.EncodeToString(h.Sum(nil))[:20]
}
