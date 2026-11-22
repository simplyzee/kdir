package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thomaschaplin/kdir/internal/discover"
	"github.com/thomaschaplin/kdir/internal/engine"
	"github.com/thomaschaplin/kdir/internal/kubeconfig"
	"github.com/thomaschaplin/kdir/internal/resolve"
	"github.com/thomaschaplin/kdir/internal/spec"
)

func newInit() *cobra.Command {
	var (
		context, regex, namespace string
		server, force             bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a .kubectx in the current directory and allow it",
		Long: `Write a .kubectx file in the current directory and allow it.

With no flags it records your current context and its namespace. --server
records the current context's API server URL instead of its name, which is
the most robust choice for a team because the URL is the same on every
machine while context names often differ.`,
		Example: `  kdir init
  kdir init --context staging-eu --namespace payments
  kdir init --regex '^arn:aws:eks:eu-west-1:123456789012:cluster/staging$'
  kdir init --server`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := engine.OSEnv(osEnvDefaultPID())
			if err != nil {
				return err
			}
			set := 0
			for _, b := range []bool{context != "", regex != "", server} {
				if b {
					set++
				}
			}
			if set > 1 {
				return errors.New("use at most one of --context, --regex and --server")
			}

			// The current context as kubectl sees it in this shell.
			cfg, err := kubeconfig.Load(kubeconfig.Files(e.Getenv(engine.EnvKubeconfig), e.Home))
			if err != nil {
				return fmt.Errorf("loading kubeconfig: %w", err)
			}
			current := cfg.CurrentContext
			currentCtx := cfg.Contexts[current]

			var b strings.Builder
			b.WriteString("# kdir: Kubernetes context for this directory. Safe to commit; contains no credentials.\n")
			switch {
			case context != "":
				fmt.Fprintf(&b, "context: %s\n", yamlString(context))
			case regex != "":
				fmt.Fprintf(&b, "contextRegex: %s\n", yamlString(regex))
			default:
				if current == "" || currentCtx == nil {
					return errors.New("no current context in your kubeconfig; use --context, --regex or --server")
				}
				if server {
					cl := cfg.Clusters[currentCtx.Cluster]
					if cl == nil || cl.Server == "" {
						return fmt.Errorf("context %q has no cluster server URL", current)
					}
					fmt.Fprintf(&b, "server: %s\n", yamlString(cl.Server))
				} else {
					fmt.Fprintf(&b, "context: %s\n", yamlString(current))
				}
			}
			ns := namespace
			if ns == "" && context == "" && regex == "" && currentCtx != nil {
				ns = currentCtx.Namespace
			}
			if ns != "" {
				fmt.Fprintf(&b, "namespace: %s\n", yamlString(ns))
			}
			data := []byte(b.String())

			s, _, err := spec.Parse(data)
			if err != nil {
				return err
			}
			if r := resolve.Resolve(s, cfg); r.Status != resolve.Found {
				warnf(cmd.ErrOrStderr(), "warning: %s", strings.Replace(r.Message(s), "kubectl is disabled", "kubectl will be disabled", 1))
			}

			path := filepath.Join(e.Dir, discover.FileName)
			flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
			if force {
				flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			}
			f, err := os.OpenFile(path, flags, 0o644)
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%s already exists; use --force to overwrite it", engine.Tilde(path, e.Home))
			}
			if err != nil {
				return err
			}
			if _, err := f.Write(data); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := engine.TrustStore(e).Allow(engine.Canonical(path), data); err != nil {
				return err
			}
			warnf(cmd.ErrOrStderr(), "wrote and allowed %s (%s)", engine.Tilde(path, e.Home), s.Describe())
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&context, "context", "", "match this exact context name")
	f.StringVar(&regex, "regex", "", "match contexts by regular expression")
	f.BoolVar(&server, "server", false, "match the current context's API server URL")
	f.StringVar(&namespace, "namespace", "", "namespace to use (default: the current context's)")
	f.BoolVar(&force, "force", false, "overwrite an existing .kubectx")
	return cmd
}

var plainYAML = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._/:@-]*$`)

// yamlString writes s as a plain YAML scalar when that is unambiguous, and
// double-quoted otherwise.
func yamlString(s string) string {
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "y", "n":
		return strconv.Quote(s)
	}
	if plainYAML.MatchString(s) && !strings.HasSuffix(s, ":") {
		return s
	}
	return strconv.Quote(s)
}
