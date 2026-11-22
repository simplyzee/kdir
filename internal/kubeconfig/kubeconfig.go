// Package kubeconfig loads kubeconfig files the way kubectl does.
package kubeconfig

import (
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Files returns the kubeconfig precedence list kubectl would use for the
// given KUBECONFIG value: the colon-separated list if it is non-empty,
// otherwise ~/.kube/config. Empty entries are dropped, as kubectl does.
func Files(kubeconfigEnv, home string) []string {
	if kubeconfigEnv != "" {
		var out []string
		for _, f := range filepath.SplitList(kubeconfigEnv) {
			if f != "" {
				out = append(out, f)
			}
		}
		if len(out) > 0 {
			return dedupe(out)
		}
	}
	return []string{filepath.Join(home, ".kube", "config")}
}

// Load merges the given files with kubectl's rules (first file to set a value
// wins). Missing files are ignored; unreadable or invalid ones are an error.
func Load(files []string) (*clientcmdapi.Config, error) {
	rules := &clientcmd.ClientConfigLoadingRules{Precedence: files}
	return rules.Load()
}

func dedupe(xs []string) []string {
	seen := make(map[string]bool, len(xs))
	out := xs[:0:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
