// Package cli implements kdir's commands.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// Build information, set with -ldflags "-X github.com/thomaschaplin/kdir/internal/cli.Version=...".
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// exitError carries a specific exit code without printing anything more.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// Main runs kdir with the given arguments and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	root := newRoot(stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	var ee exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.code
	default:
		_, _ = fmt.Fprintf(stderr, "kdir: %v\n", err)
		return 1
	}
}

func newRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "kdir",
		Short: "Per-directory Kubernetes context and namespace, from a committable .kubectx file",
		Long: `kdir switches your Kubernetes context and namespace when you cd into a
directory with a .kubectx file, for the current shell only.

Add the hook to your shell's rc file:
  zsh:  eval "$(kdir hook zsh)"
  bash: eval "$(kdir hook bash)"`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.CompletionOptions.HiddenDefaultCmd = true
	root.AddCommand(
		newExport(),
		newHook(),
		newStatus(),
		newAllow(),
		newDeny(),
		newInit(),
		newExec(),
		newGC(),
		newGuard(),
		newVersion(),
	)
	return root
}

func newVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "kdir %s (commit %s, built %s)\n", Version, Commit, Date)
			return err
		},
	}
}

// warnf prints a one-line notice to stderr.
func warnf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, "kdir: "+format+"\n", args...)
}

func osEnvDefaultPID() int { return os.Getppid() }
