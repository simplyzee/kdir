package shell

import (
	"fmt"
	"strings"

	"github.com/thomaschaplin/kdir/internal/engine"
)

type fish struct{}

func (fish) Name() string { return "fish" }

// The fish hook runs when PWD changes and before each prompt. It uses
// $fish_pid so each shell gets its own overlay. Event handlers do not change
// the $status that fish_prompt sees.
func (fish) Hook() string {
	return `function __kdir_hook --on-variable PWD --on-event fish_prompt --description 'kdir: Kubernetes context for this directory'
    command kdir export fish --pid $fish_pid | source
end
`
}

func (fish) Export(changes []engine.Change) string {
	var b strings.Builder
	for _, c := range changes {
		checkName(c.Name)
		if c.Unset {
			fmt.Fprintf(&b, "set -e %s;\n", c.Name)
		} else {
			fmt.Fprintf(&b, "set -gx %s %s;\n", c.Name, QuoteFish(c.Value))
		}
	}
	return b.String()
}

// QuoteFish single-quotes s for fish, where backslash and the single quote
// are the only characters that are special inside single quotes.
func QuoteFish(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

func init() { register(fish{}) }
