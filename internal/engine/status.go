package engine

import (
	"os"

	"sigs.k8s.io/yaml"

	"github.com/thomaschaplin/kdir/internal/overlay"
	"github.com/thomaschaplin/kdir/internal/state"
)

// OriginalKubeconfig returns the user's own KUBECONFIG: the value kdir saved
// if it is managing this shell, otherwise the current one.
func OriginalKubeconfig(e Env) Original { return readShell(e).orig }

// ActiveOverlay returns this shell's overlay if kdir has applied the given
// governing file in it, or "".
func ActiveOverlay(e Env, kubectxPath string) string {
	st, ok := state.Decode(e.Getenv(EnvState))
	if !ok || st.File != kubectxPath || st.Mode == state.NotAllowed {
		return ""
	}
	return e.Getenv(EnvOverlay)
}

// Short returns "context:namespace" (or "context" when there is no
// namespace) for prompts, or "" when kdir has not selected a context in this
// shell. It reads only KDIR_STATE and this shell's tiny overlay, so it
// reflects in-shell `kubectl config use-context` / kubens changes without
// loading the user's kubeconfig.
func Short(e Env) string {
	st, ok := state.Decode(e.Getenv(EnvState))
	if !ok || st.Mode != state.Active {
		return ""
	}
	data, err := os.ReadFile(e.Getenv(EnvOverlay))
	if err != nil {
		return ""
	}
	var f overlay.File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return ""
	}
	for _, c := range f.Contexts {
		if c.Name == f.CurrentContext {
			if c.Context.Namespace != "" {
				return c.Name + ":" + c.Context.Namespace
			}
			return c.Name
		}
	}
	return ""
}
