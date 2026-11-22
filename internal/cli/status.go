package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thomaschaplin/kdir/internal/discover"
	"github.com/thomaschaplin/kdir/internal/engine"
	"github.com/thomaschaplin/kdir/internal/resolve"
	"github.com/thomaschaplin/kdir/internal/state"
	"github.com/thomaschaplin/kdir/internal/trust"
)

// Exit codes for `kdir status`.
const (
	statusActive     = 0
	statusUngoverned = 1
	statusFailClosed = 2
	statusNotAllowed = 3
)

func newStatus() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what kdir is doing in this directory",
		Long: `Show the governing .kubectx file and what kdir resolved it to.

Exit status: 0 active, 1 no .kubectx governs this directory, 2 fail-closed
(kubectl is disabled), 3 the .kubectx is not allowed.

--short prints "context:namespace" for use in prompts, or nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := engine.OSEnv(osEnvDefaultPID())
			if err != nil {
				return err
			}
			if short {
				if s := engine.Short(e); s != "" {
					_, err = fmt.Fprintln(cmd.OutOrStdout(), s)
				}
				return err
			}
			return status(cmd, e)
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, `print "context:namespace" for prompts`)
	return cmd
}

func status(cmd *cobra.Command, e engine.Env) error {
	out := cmd.OutOrStdout()
	found, err := discover.Find(e.Dir, engine.Stops(e.Home)...)
	if err != nil {
		return err
	}
	if found == nil {
		_, _ = fmt.Fprintf(out, "No .kubectx governs %s; kubectl uses your normal kubeconfig.\n", engine.Tilde(e.Dir, e.Home))
		return exitError{statusUngoverned}
	}
	d, err := engine.Evaluate(e, found, engine.OriginalKubeconfig(e), engine.TrustStore(e))
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	row := func(k, v string) { _, _ = fmt.Fprintf(tw, "%s:\t%s\n", k, v) } // errors surface from Flush
	row("file", engine.Tilde(found.Path, e.Home))
	switch d.Trust {
	case trust.Allowed:
		row("allowed", "yes")
	case trust.Changed:
		row("allowed", "no, changed since it was allowed (run 'kdir allow')")
	default:
		row("allowed", "no (run 'kdir allow')")
	}
	code := statusActive
	if d.Mode == state.NotAllowed {
		code = statusNotAllowed
	} else {
		if d.Spec != nil {
			row("matcher", d.Spec.Describe())
		}
		for _, w := range d.Warnings {
			row("warning", w)
		}
		switch {
		case d.Mode == state.Active:
			row("context", d.Context)
		case d.Result.Status == resolve.Ambiguous:
			row("context", fmt.Sprintf("ambiguous: %s (kubectl disabled)", strings.Join(d.Result.Candidates, ", ")))
		default:
			row("context", "none, kubectl disabled: "+d.Problem)
		}
		if d.Mode == state.Active {
			ns := d.Namespace
			if ns == "" {
				ns = "(none; kubectl uses \"default\")"
			}
			row("namespace", ns)
		}
		if d.Spec != nil {
			row("protect", fmt.Sprint(d.Spec.Protect))
		}
		if d.Mode == state.FailClosed {
			code = statusFailClosed
		}
	}
	if ov := engine.ActiveOverlay(e, found.Path); ov != "" {
		row("overlay", ov)
	} else {
		row("overlay", "not applied in this shell (is the hook installed? see 'kdir hook --help')")
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if code != statusActive {
		return exitError{code}
	}
	return nil
}
