package shell

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/thomaschaplin/kdir/internal/engine"
)

var tricky = []string{
	"",
	"plain",
	"/Users/me/src/pay ments/.kubectx",
	"it's",
	`"double"`,
	"$HOME and ${HOME} and $(echo pwned) and `echo pwned`",
	`back\slash\n`,
	"line1\nline2",
	"tab\there",
	"semi;colon && pipe | amp & glob * ? [x]",
	"''''",
	"!history !! ~tilde",
	"unicode: ünïcødé ☸",
	"/a:/b c:/d'e",
}

func TestExportRoundTripsInRealShells(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			bin, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s not installed", name)
			}
			sh, _ := Get(name)
			for _, v := range tricky {
				code := sh.Export([]engine.Change{{Name: "KDIR_TEST", Value: v}, {Name: "KDIR_GONE", Unset: true}})
				script := "KDIR_GONE=x; eval \"$1\"; printf '%s|%s' \"$KDIR_TEST\" \"${KDIR_GONE-unset}\"; env | grep -q '^KDIR_TEST=' && printf '|exported'"
				args := []string{"-c", script, "kdir-test", code}
				if name == "zsh" {
					args = append([]string{"-f"}, args...)
				} else {
					args = append([]string{"--norc", "--noprofile"}, args...)
				}
				out, err := exec.Command(bin, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("%q: %v\n%s", v, err, out)
				}
				want := v + "|unset|exported"
				if string(out) != want {
					t.Errorf("value %q came back as %q\ncode: %s", v, out, code)
				}
			}
		})
	}
}

func TestExportFormat(t *testing.T) {
	sh, _ := Get("bash")
	got := sh.Export([]engine.Change{{Name: "A", Value: "it's"}, {Name: "B", Unset: true}})
	want := "export A='it'\\''s';\nunset B;\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInvalidNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	sh, _ := Get("zsh")
	sh.Export([]engine.Change{{Name: "A;rm", Value: "x"}})
}

func TestGet(t *testing.T) {
	if _, err := Get("tcsh"); err == nil || !strings.Contains(err.Error(), "bash, zsh") {
		t.Fatalf("err = %v", err)
	}
}

// The bash hook must install into PROMPT_COMMAND without clobbering an
// existing string or array value, and only once.
func TestBashHookInstall(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	sh, _ := Get("bash")
	hook := sh.Hook()
	tests := []struct {
		name, setup, want string
	}{
		{"unset", "unset PROMPT_COMMAND", "_kdir_hook"},
		{"empty", "PROMPT_COMMAND=", "_kdir_hook"},
		{"existing string", "PROMPT_COMMAND='history -a'", "_kdir_hook;history -a"},
		{"existing string with trailing semicolon", "PROMPT_COMMAND='a;'", "_kdir_hook;a;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := tt.setup + "\neval \"$1\"\neval \"$1\"\nprintf '%s' \"$PROMPT_COMMAND\""
			out, err := exec.Command(bash, "--norc", "--noprofile", "-c", script, "x", hook).CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if string(out) != tt.want {
				t.Errorf("PROMPT_COMMAND = %q, want %q", out, tt.want)
			}
		})
	}
	// Array form (bash >= 5.1) only where supported.
	out, err := exec.Command(bash, "--norc", "--noprofile", "-c",
		`PROMPT_COMMAND=(a b); eval "$1"; eval "$1"; printf '%s,' "${PROMPT_COMMAND[@]}"`, "x", hook).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if string(out) != "_kdir_hook,a,b," {
		t.Errorf("array PROMPT_COMMAND = %q", out)
	}
}

func TestZshHookInstall(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	sh, _ := Get("zsh")
	script := `precmd_functions=(mine); eval "$1"; eval "$1"; print -r -- "${precmd_functions[*]}|${chpwd_functions[*]}"`
	out, err := exec.Command(zsh, "-f", "-c", script, "x", sh.Hook()).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "_kdir_hook mine|_kdir_hook" {
		t.Errorf("got %q", got)
	}
}

// The hook must preserve $? for prompts that display the last exit status,
// even when kdir itself is missing or fails.
func TestHooksPreserveExitStatus(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		sh, _ := Get(name)
		flag := "-f"
		if name == "bash" {
			flag = "--norc"
		}
		script := `eval "$1"; PATH=/nonexistent; (exit 42); _kdir_hook 2>/dev/null; echo $?`
		out, err := exec.Command(bin, flag, "-c", script, "x", sh.Hook()).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if strings.TrimSpace(string(out)) != "42" {
			t.Errorf("%s: $? after hook = %q, want 42", name, out)
		}
	}
}
