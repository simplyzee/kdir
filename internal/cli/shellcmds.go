package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/thomaschaplin/kdir/internal/engine"
	"github.com/thomaschaplin/kdir/internal/shell"
)

func newHook() *cobra.Command {
	return &cobra.Command{
		Use:       "hook <shell>",
		Short:     "Print the shell hook to eval from your rc file",
		Example:   "  eval \"$(kdir hook zsh)\"\n  eval \"$(kdir hook bash)\"",
		Args:      cobra.ExactArgs(1),
		ValidArgs: shell.Names(),
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, err := shell.Get(args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), sh.Hook())
			return err
		},
	}
}

func newExport() *cobra.Command {
	var pid int
	cmd := &cobra.Command{
		Use:    "export <shell>",
		Short:  "Print shell code that updates the environment for the current directory (used by the hook)",
		Args:   cobra.ExactArgs(1),
		Hidden: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			stderr := cmd.ErrOrStderr()
			sh, err := shell.Get(args[0])
			if err != nil {
				return err
			}
			if pid <= 0 {
				pid = osEnvDefaultPID()
			}
			e, err := engine.OSEnv(pid)
			if err != nil {
				return err
			}
			res, err := engine.Export(e)
			if err != nil {
				// Never break the prompt: one line on stderr, no shell code.
				return err
			}
			for _, m := range res.Messages {
				warnf(stderr, "%s", m)
			}
			if len(res.Changes) == 0 {
				return nil
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), sh.Export(res.Changes))
			return err
		},
	}
	cmd.Flags().IntVar(&pid, "pid", 0, "PID of the shell being managed (default: parent process)")
	return cmd
}
