package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/thomaschaplin/kdir/internal/cli"
	"github.com/thomaschaplin/kdir/internal/testutil"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"kdir": func() { os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr)) },
		"kctx": kctx,
	})
}

// lib.sh is sourced by every scenario. It installs the real hook and defines
// helpers that work in both zsh and bash (including macOS's bash 3.2).
const lib = `
if [ -n "${ZSH_VERSION:-}" ]; then
  eval "$(kdir hook zsh)"
  prompt() { local f; for f in $precmd_functions; do $f; done; }
else
  eval "$(kdir hook bash)"
  prompt() { eval "$PROMPT_COMMAND"; }
fi
# go to a directory and draw a prompt, like an interactive user would
to() { cd "$1" && prompt; }
# print what kubectl would use; cross-check against the real kubectl if present
check() {
  local k kc
  k=$(kctx)
  if command -v kubectl >/dev/null 2>&1; then
    kc=$(kubectl config view --minify -o 'jsonpath={.current-context}/{..namespace}' 2>/dev/null) || kc=ERROR
    [ "$kc" = "$k" ] || echo "kubectl disagrees: $kc (kctx: $k)"
  fi
  echo "$1: $k"
}
kdirvars() { env | grep '^KDIR_' | sed 's/=.*//' | sort | paste -sd ' ' -; }
`

func TestScripts(t *testing.T) {
	for _, sh := range []struct{ name, flag, iflag1, iflag2 string }{
		{"zsh", "-f", "--no-globalrcs", "-i"},
		{"bash", "--norc", "--noprofile", "-i"},
	} {
		t.Run(sh.name, func(t *testing.T) {
			bin, err := exec.LookPath(sh.name)
			if err != nil {
				t.Skipf("%s not installed", sh.name)
			}
			testscript.Run(t, testscript.Params{
				Dir:                 filepath.Join(testutil.Root(), "testdata", "script"),
				RequireExplicitExec: true,
				Setup: func(env *testscript.Env) error {
					home := filepath.Join(env.WorkDir, "home")
					kube := filepath.Join(home, ".kube")
					if err := os.MkdirAll(kube, 0o755); err != nil {
						return err
					}
					basic, err := os.ReadFile(testutil.Fixture("kubeconfig/basic.yaml"))
					if err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(kube, "config"), basic, 0o600); err != nil {
						return err
					}
					for _, f := range []string{"multi-a.yaml", "multi-b.yaml"} {
						data, err := os.ReadFile(testutil.Fixture("kubeconfig/" + f))
						if err != nil {
							return err
						}
						if err := os.WriteFile(filepath.Join(env.WorkDir, f), data, 0o600); err != nil {
							return err
						}
					}
					if err := os.WriteFile(filepath.Join(env.WorkDir, "lib.sh"), []byte(lib), 0o644); err != nil {
						return err
					}
					env.Setenv("HOME", home)
					env.Setenv("XDG_RUNTIME_DIR", filepath.Join(env.WorkDir, "run"))
					env.Setenv("SHELLBIN", bin)
					env.Setenv("SHELLFLAG", sh.flag)
					env.Setenv("SHELLNAME", sh.name)
					env.Setenv("SHELLIFLAG1", sh.iflag1)
					env.Setenv("SHELLIFLAG2", sh.iflag2)
					return nil
				},
			})
		})
	}
}

// kctx is a test helper that reads kubeconfig exactly like kubectl (client-go
// default loading rules: KUBECONFIG or ~/.kube/config).
//
//	kctx              print "context/namespace", or "ERROR" if kubectl would fail
//	kctx use NAME     like kubectl config use-context NAME
//	kctx ns NAME      like kubectl config set-context --current --namespace NAME
//	kctx addcreds     like aws eks update-kubeconfig: add a cluster and user
//	kctx stamp FILE.. print sha256 and mtime of files
func kctx() {
	args := os.Args[1:]
	if len(args) == 0 {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
		if _, err := cc.ClientConfig(); err != nil {
			fmt.Println("ERROR")
			return
		}
		raw, err := cc.RawConfig()
		if err != nil {
			fmt.Println("ERROR")
			return
		}
		fmt.Printf("%s/%s\n", raw.CurrentContext, raw.Contexts[raw.CurrentContext].Namespace)
		return
	}
	modify := func(fn func(*clientcmdapi.Config)) {
		po := clientcmd.NewDefaultPathOptions()
		cfg, err := po.GetStartingConfig()
		if err != nil {
			fail(err)
		}
		fn(cfg)
		if err := clientcmd.ModifyConfig(po, *cfg, true); err != nil {
			fail(err)
		}
	}
	switch args[0] {
	case "use":
		modify(func(c *clientcmdapi.Config) { c.CurrentContext = args[1] })
	case "ns":
		modify(func(c *clientcmdapi.Config) { c.Contexts[c.CurrentContext].Namespace = args[1] })
	case "addcreds":
		modify(func(c *clientcmdapi.Config) {
			c.Clusters["new-eks"] = &clientcmdapi.Cluster{Server: "https://new.example.com"}
			c.AuthInfos["new-eks"] = &clientcmdapi.AuthInfo{Token: "fake"}
		})
	case "stamp":
		for _, f := range args[1:] {
			data, err := os.ReadFile(f)
			if err != nil {
				fail(err)
			}
			fi, err := os.Stat(f)
			if err != nil {
				fail(err)
			}
			fmt.Printf("%s %x %d\n", filepath.Base(f), sha256.Sum256(data), fi.ModTime().UnixNano())
		}
	default:
		fail(fmt.Errorf("unknown kctx command %q", args[0]))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "kctx:", err)
	os.Exit(1)
}
