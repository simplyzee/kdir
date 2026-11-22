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
    __kdir_protect_sync
end
function __kdir_protect_sync --description 'kdir: add or remove the protect: true kubectl wrapper'
    if set -q KDIR_PROTECT
        set -q __kdir_wrapped; and return 0
        if functions -q kubectl
            echo "kdir: protect: true is set here, but you already have a kubectl function or alias; kdir won't replace it" >&2
            set -g __kdir_wrapped foreign
        else
            function kubectl --description 'kdir protect: true wrapper'
                if string match -q -- '__complete*' "$argv[1]"
                    command kubectl $argv
                    return
                end
                command kdir guard -- $argv; and command kubectl $argv
            end
            set -g __kdir_wrapped 1
        end
    else if set -q __kdir_wrapped
        test "$__kdir_wrapped" = 1; and functions -e kubectl
        set -e __kdir_wrapped
    end
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
