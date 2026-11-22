package engine

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/thomaschaplin/kdir/internal/state"
)

// ErrNotAllowed is returned by Prepare when the governing .kubectx is not
// trusted.
var ErrNotAllowed = errors.New("not allowed")

// Prepared is the environment for running a single command under kdir
// without a shell hook.
type Prepared struct {
	Environ  []string // full environment for the command
	Messages []string
	overlay  string
	env      Env
}

// Prepare computes the environment a hooked shell would have in e.Dir and
// writes an overlay for e.PID (normally kdir's own PID). Call Cleanup when
// the command has finished.
//
// An untrusted .kubectx is an error rather than being silently ignored: a
// script that expects kdir to select a context must not quietly run against
// whatever context is active instead.
func Prepare(e Env, environ []string) (*Prepared, error) {
	r, err := Export(e)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{}
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	p := &Prepared{Messages: r.Messages, env: e}
	for _, c := range r.Changes {
		if c.Unset {
			delete(vars, c.Name)
			continue
		}
		vars[c.Name] = c.Value
		if c.Name == EnvState {
			if st, ok := state.Decode(c.Value); ok && st.Mode == state.NotAllowed {
				return nil, fmt.Errorf(".kubectx in %s is %w; run 'kdir allow' first", Tilde(dirOf(st.File), e.Home), ErrNotAllowed)
			}
		}
		if c.Name == EnvOverlay {
			p.overlay = c.Value
		}
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p.Environ = append(p.Environ, k+"="+vars[k])
	}
	return p, nil
}

// Cleanup removes the overlay Prepare wrote, keeping it if another tool
// added credentials to it. It returns any notices for stderr.
func (p *Prepared) Cleanup() []string {
	if p.overlay == "" {
		return nil
	}
	var r Result
	discard(&r, p.overlay, p.env)
	return r.Messages
}

func dirOf(path string) string {
	if i := strings.LastIndexByte(path, os.PathSeparator); i > 0 {
		return path[:i]
	}
	return path
}
