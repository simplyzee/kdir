// Package resolve matches a .kubectx spec against a loaded kubeconfig.
package resolve

import (
	"fmt"
	"sort"
	"strings"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/thomaschaplin/kdir/internal/spec"
)

// Status is the outcome of resolving a spec.
type Status int

const (
	// Found means exactly one context matched.
	Found Status = iota
	// Missing means no context matched.
	Missing
	// Ambiguous means more than one context matched.
	Ambiguous
)

func (s Status) String() string {
	switch s {
	case Found:
		return "found"
	case Missing:
		return "missing"
	case Ambiguous:
		return "ambiguous"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Result is the outcome of Resolve.
type Result struct {
	Status Status
	// Context is the matched context name when Status is Found.
	Context string
	// Namespace is the effective namespace when Status is Found: the spec's
	// namespace, else the context's own, else "".
	Namespace string
	// Candidates lists the matching contexts (sorted) when Status is Ambiguous.
	Candidates []string
}

// Resolve finds the context the spec asks for.
func Resolve(s *spec.Spec, cfg *clientcmdapi.Config) Result {
	var matches []string
	if s.Context != "" {
		if _, ok := cfg.Contexts[s.Context]; ok {
			matches = []string{s.Context}
		}
	} else {
		var wantServer string
		if s.Server != "" {
			wantServer = spec.NormalizeServer(s.Server)
		}
		for name, c := range cfg.Contexts {
			if c == nil {
				continue
			}
			if re := s.Regexp(); re != nil && !re.MatchString(name) {
				continue
			}
			if s.Server != "" {
				cl, ok := cfg.Clusters[c.Cluster]
				if !ok || cl == nil || spec.NormalizeServer(cl.Server) != wantServer {
					continue
				}
			}
			if s.User != "" && c.AuthInfo != s.User {
				continue
			}
			matches = append(matches, name)
		}
		sort.Strings(matches)
	}

	switch len(matches) {
	case 0:
		return Result{Status: Missing}
	case 1:
		ns := s.Namespace
		if ns == "" {
			ns = cfg.Contexts[matches[0]].Namespace
		}
		return Result{Status: Found, Context: matches[0], Namespace: ns}
	default:
		return Result{Status: Ambiguous, Candidates: matches}
	}
}

// Message returns the one-line, user-facing explanation for a non-Found
// result, without the "kdir: " prefix.
func (r Result) Message(s *spec.Spec) string {
	switch r.Status {
	case Missing:
		if s.Context != "" {
			return fmt.Sprintf(".kubectx wants context %q but it isn't in your kubeconfig. kubectl is disabled in this directory until it is.", s.Context)
		}
		return fmt.Sprintf(".kubectx wants %s but no context in your kubeconfig matches. kubectl is disabled in this directory until one does.", s.Describe())
	case Ambiguous:
		return fmt.Sprintf(".kubectx wants %s but %d contexts match (%s). kubectl is disabled in this directory until the match is unique.", s.Describe(), len(r.Candidates), strings.Join(r.Candidates, ", "))
	}
	return ""
}
