package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/thomaschaplin/kdir/internal/discover"
	"github.com/thomaschaplin/kdir/internal/engine"
)

func newAllow() *cobra.Command {
	return &cobra.Command{
		Use:   "allow [path]",
		Short: "Trust a .kubectx file (default: the one governing this directory)",
		Long: `Trust a .kubectx file so kdir will act on it. path may be the file or its
directory; by default it is the .kubectx governing the current directory.

kdir records the file's path and contents. If either changes, the file must
be allowed again.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := engine.OSEnv(osEnvDefaultPID())
			if err != nil {
				return err
			}
			path, err := target(e, args, true)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := engine.TrustStore(e).Allow(engine.Canonical(path), data); err != nil {
				return err
			}
			warnf(cmd.ErrOrStderr(), "allowed %s", engine.Tilde(path, e.Home))
			return nil
		},
	}
}

func newDeny() *cobra.Command {
	return &cobra.Command{
		Use:   "deny [path]",
		Short: "Stop trusting a .kubectx file (default: the one governing this directory)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := engine.OSEnv(osEnvDefaultPID())
			if err != nil {
				return err
			}
			path, err := target(e, args, false)
			if err != nil {
				return err
			}
			if err := engine.TrustStore(e).Deny(engine.Canonical(path)); err != nil {
				return err
			}
			warnf(cmd.ErrOrStderr(), "denied %s", engine.Tilde(path, e.Home))
			return nil
		},
	}
}

// target resolves the .kubectx file an allow/deny command refers to.
func target(e engine.Env, args []string, mustExist bool) (string, error) {
	if len(args) == 0 {
		found, err := discover.Find(e.Dir, engine.Stops(e.Home)...)
		if err != nil {
			return "", err
		}
		if found == nil {
			return "", fmt.Errorf("no %s in %s or its parents", discover.FileName, engine.Tilde(e.Dir, e.Home))
		}
		return found.Path, nil
	}
	p, err := filepath.Abs(args[0])
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	switch {
	case err == nil && fi.IsDir():
		p = filepath.Join(p, discover.FileName)
	case err == nil:
		// a file
	case errors.Is(err, os.ErrNotExist) && !mustExist:
		// deny of a deleted file: keep the path as given
	default:
		return "", err
	}
	if mustExist {
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
	}
	return p, nil
}
