package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/thomaschaplin/kdir/internal/engine"
)

func newExec() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec -- <command> [args...]",
		Short: "Run a command with this directory's Kubernetes context, without a shell hook",
		Long: `Run a command with the environment a hooked shell would have in the current
directory. Use it in scripts, CI jobs and Makefiles.

The .kubectx must be allowed ('kdir allow'); otherwise kdir exec refuses to
run rather than letting the command use whatever context is active. If the
context can't be resolved, the command runs with kubectl disabled, exactly
as in a shell.`,
		Example: `  kdir exec -- kubectl get pods
  kdir exec -- helm upgrade --install app ./chart`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stderr := cmd.ErrOrStderr()
			e, err := engine.OSEnv(os.Getpid())
			if err != nil {
				return err
			}
			p, err := engine.Prepare(e, os.Environ())
			if err != nil {
				return err
			}
			for _, m := range p.Messages {
				warnf(stderr, "%s", m)
			}
			defer func() {
				for _, m := range p.Cleanup() {
					warnf(stderr, "%s", m)
				}
			}()

			path, err := exec.LookPath(args[0])
			if err != nil {
				return err
			}
			c := exec.Command(path, args[1:]...)
			c.Args[0] = args[0]
			c.Env = p.Environ
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, cmd.OutOrStdout(), stderr

			// The terminal delivers Ctrl-C and Ctrl-\ to the whole process
			// group, so ignore them here and let the child decide; forward
			// signals that are sent to kdir alone.
			signal.Ignore(syscall.SIGINT, syscall.SIGQUIT)
			sigs := make(chan os.Signal, 4)
			signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2)
			defer signal.Stop(sigs)

			if err := c.Start(); err != nil {
				return err
			}
			done := make(chan struct{})
			go func() {
				for {
					select {
					case s := <-sigs:
						_ = c.Process.Signal(s)
					case <-done:
						return
					}
				}
			}()
			err = c.Wait()
			close(done)

			var ee *exec.ExitError
			if errors.As(err, &ee) {
				if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					return exitError{128 + int(ws.Signal())}
				}
				return exitError{ee.ExitCode()}
			}
			return err
		},
	}
	// Everything after the command name belongs to the command, so
	// "kdir exec kubectl get pods -n x" works without "--" too.
	cmd.Flags().SetInterspersed(false)
	return cmd
}
