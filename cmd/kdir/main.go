// Command kdir switches the Kubernetes context and namespace per directory,
// for the current shell only, from a committable .kubectx file.
package main

import (
	"os"

	"github.com/thomaschaplin/kdir/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
