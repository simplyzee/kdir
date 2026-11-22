// Package shell renders kdir's hook scripts and environment changes for each
// supported shell.
package shell

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/thomaschaplin/kdir/internal/engine"
)

// Shell knows one shell's syntax.
type Shell interface {
	Name() string
	// Hook returns code that installs the prompt hook.
	Hook() string
	// Export renders changes as code to eval.
	Export(changes []engine.Change) string
}

var shells = map[string]Shell{}

func register(s Shell) { shells[s.Name()] = s }

// Get returns the named shell.
func Get(name string) (Shell, error) {
	if s, ok := shells[name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("unsupported shell %q (supported: %s)", name, strings.Join(Names(), ", "))
}

// Names lists supported shells.
func Names() []string {
	var out []string
	for n := range shells {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

var validName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func checkName(n string) {
	if !validName.MatchString(n) {
		// Names come from kdir's own constants, never from user input.
		panic(fmt.Sprintf("shell: invalid variable name %q", n))
	}
}

// posix covers bash and zsh, which share export/unset syntax.
type posix struct {
	name string
	hook string
}

func (p posix) Name() string { return p.name }
func (p posix) Hook() string { return p.hook }

func (p posix) Export(changes []engine.Change) string {
	var b strings.Builder
	for _, c := range changes {
		checkName(c.Name)
		if c.Unset {
			fmt.Fprintf(&b, "unset %s;\n", c.Name)
		} else {
			fmt.Fprintf(&b, "export %s=%s;\n", c.Name, QuotePOSIX(c.Value))
		}
	}
	return b.String()
}

// QuotePOSIX single-quotes s for bash and zsh. Inside single quotes nothing
// is special except the quote itself, which is written as: quote, backslash,
// quote, quote.
func QuotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func init() {
	register(posix{name: "bash", hook: bashHook})
	register(posix{name: "zsh", hook: zshHook})
}

// The hooks call `command kdir` so a user function or alias named kdir is
// bypassed, and pass the shell's own PID so each shell gets its own overlay.
// They preserve $? so prompts that show the last exit status keep working.
//
// After applying the environment, _kdir_protect_sync defines a kubectl
// function while KDIR_PROTECT is set (protect: true) and removes it when it
// is not, without starting a process. The function asks `kdir guard` before
// mutating commands. Completion requests (kubectl __complete ...) go straight
// to kubectl, and the completion functions stay bound to the name "kubectl",
// so tab completion keeps working. kdir never replaces a kubectl alias or
// function the user defined; it says once that protect can't apply.

// protectPOSIX is shared by bash and zsh; hasFunc is the shell's test for an
// existing kubectl function.
func protectPOSIX(hasFunc string) string {
	return `_kdir_protect_sync() {
  if [ -n "${KDIR_PROTECT:-}" ]; then
    [ -n "${_kdir_wrapped:-}" ] && return 0
    if alias kubectl >/dev/null 2>&1; then
      echo "kdir: protect: true is set here, but kubectl is an alias, which bypasses kdir's confirmation" >&2
      _kdir_wrapped=foreign
    elif ` + hasFunc + `; then
      echo "kdir: protect: true is set here, but you already have a kubectl function; kdir won't replace it" >&2
      _kdir_wrapped=foreign
    else
      # "function name" form: a kubectl alias defined later can't rename it.
      function kubectl {
        case "${1:-}" in __complete*) command kubectl "$@"; return ;; esac
        command kdir guard -- "$@" && command kubectl "$@"
      }
      _kdir_wrapped=1
    fi
  elif [ -n "${_kdir_wrapped:-}" ]; then
    [ "$_kdir_wrapped" = 1 ] && unset -f kubectl
    unset _kdir_wrapped
  fi
}
`
}

var zshHook = `_kdir_hook() {
  local _kdir_status=$?
  eval "$(command kdir export zsh --pid $$)"
  _kdir_protect_sync
  return $_kdir_status
}
` + protectPOSIX("(( ${+functions[kubectl]} ))") + `typeset -ag precmd_functions chpwd_functions
if (( ! ${precmd_functions[(I)_kdir_hook]} )); then
  precmd_functions=(_kdir_hook $precmd_functions)
fi
if (( ! ${chpwd_functions[(I)_kdir_hook]} )); then
  chpwd_functions=(_kdir_hook $chpwd_functions)
fi
`

var bashHook = `_kdir_hook() {
  local _kdir_status=$?
  eval "$(command kdir export bash --pid $$)"
  _kdir_protect_sync
  return $_kdir_status
}
` + protectPOSIX("declare -F kubectl >/dev/null") + `_kdir_installed=
for _kdir_pc in "${PROMPT_COMMAND[@]:-}"; do
  case ";$_kdir_pc;" in *";_kdir_hook;"*) _kdir_installed=1 ;; esac
done
if [[ -z $_kdir_installed ]]; then
  if [[ "$(declare -p PROMPT_COMMAND 2>&1)" == "declare -a"* ]]; then
    PROMPT_COMMAND=(_kdir_hook "${PROMPT_COMMAND[@]}")
  else
    PROMPT_COMMAND="_kdir_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  fi
fi
unset _kdir_installed _kdir_pc
`
