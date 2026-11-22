package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thomaschaplin/kdir/internal/engine"
	"github.com/thomaschaplin/kdir/internal/kubeconfig"
	"github.com/thomaschaplin/kdir/internal/protect"
	"github.com/thomaschaplin/kdir/internal/shell"
)

// EnvYes skips protect confirmations.
const EnvYes = "KDIR_YES"

// envTTY overrides the terminal protect reads its answer from (tests only).
const envTTY = "KDIR_TTY"

func newGuard() *cobra.Command {
	return &cobra.Command{
		Use:   "guard -- <kubectl args...>",
		Short: "Confirm a mutating kubectl command (used by the protect wrapper)",
		Long: `Used by the kubectl shell function kdir defines in directories whose
.kubectx sets 'protect: true'. Exits 0 if the command may run: it is not
mutating, it is a --dry-run, KDIR_YES=1 is set, or the user answered y.`,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			inv := protect.Parse(args)
			if !inv.NeedsConfirmation() || os.Getenv(EnvYes) == "1" || os.Getenv(engine.EnvProtect) == "" {
				return nil
			}
			ctx, ns := effectiveTarget(inv)
			question := fmt.Sprintf("⚠ %s/%s: kubectl %s. Continue? [y/N] ", ctx, ns, shellJoin(args))

			in, out, closeTTY, err := openTTY(cmd.ErrOrStderr())
			if err != nil {
				warnf(cmd.ErrOrStderr(), "protect: no terminal to confirm 'kubectl %s'; set %s=1 to run it anyway", shellJoin(args), EnvYes)
				return exitError{1}
			}
			defer closeTTY()
			_, _ = io.WriteString(out, question)
			answer, _ := bufio.NewReader(in).ReadString('\n')
			switch strings.ToLower(strings.TrimSpace(answer)) {
			case "y", "yes":
				return nil
			}
			warnf(cmd.ErrOrStderr(), "not running kubectl %s", inv.Verb)
			return exitError{1}
		},
	}
}

// openTTY returns the terminal to ask on. The question goes to the terminal
// rather than stdout or stderr so it is seen even when kubectl's output is
// piped or redirected.
func openTTY(stderr io.Writer) (io.Reader, io.Writer, func(), error) {
	if p := os.Getenv(envTTY); p != "" {
		f, err := os.Open(p)
		if err != nil {
			return nil, nil, nil, err
		}
		return f, stderr, func() { _ = f.Close() }, nil
	}
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	return f, f, func() { _ = f.Close() }, nil
}

// effectiveTarget returns the context and namespace the command will act on,
// honouring --context, -n and -A on the command line.
func effectiveTarget(inv protect.Invocation) (ctx, ns string) {
	home, _ := os.UserHomeDir()
	cfg, err := kubeconfig.Load(kubeconfig.Files(os.Getenv(engine.EnvKubeconfig), home))
	ctx = inv.Context
	if ctx == "" {
		ctx = "?"
		if err == nil && cfg.CurrentContext != "" {
			ctx = cfg.CurrentContext
		}
	}
	switch {
	case inv.AllNS:
		ns = "*"
	case inv.Namespace != "":
		ns = inv.Namespace
	case err == nil && cfg.Contexts[ctx] != nil && cfg.Contexts[ctx].Namespace != "":
		ns = cfg.Contexts[ctx].Namespace
	default:
		ns = "default"
	}
	return ctx, ns
}

var safeWord = regexp.MustCompile(`^[A-Za-z0-9_./:=,@%+-]+$`)

func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if safeWord.MatchString(a) {
			out[i] = a
		} else {
			out[i] = shell.QuotePOSIX(a)
		}
	}
	return strings.Join(out, " ")
}
