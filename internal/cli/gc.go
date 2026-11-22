package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/thomaschaplin/kdir/internal/engine"
	"github.com/thomaschaplin/kdir/internal/overlay"
)

func newGC() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Delete overlay files left by shells that have exited",
		Long: `Delete per-shell overlay files whose shell is no longer running. kdir also
does this on its own at most once a day.

Overlays that another tool (aws eks update-kubeconfig, gcloud, kind,
kubectl config set-credentials) wrote clusters or users into are never
deleted; they are kept as kept-*.yaml and listed here so you can merge them
into your kubeconfig or delete them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stderr := cmd.ErrOrStderr()
			e, err := engine.OSEnv(os.Getpid())
			if err != nil {
				return err
			}
			dir, err := overlay.Dir(e.Getenv)
			if err != nil {
				return err
			}
			res, err := overlay.GC(dir)
			if err != nil {
				return err
			}
			warnf(stderr, "removed %d stale overlay(s) from %s", len(res.Removed), engine.Tilde(dir, e.Home))
			for _, k := range append(res.Saved, res.Kept...) {
				warnf(stderr, "kept %s: contains clusters/users written by another tool. Merge with: KUBECONFIG=~/.kube/config:%s kubectl config view --flatten", k, k)
			}
			return nil
		},
	}
}
