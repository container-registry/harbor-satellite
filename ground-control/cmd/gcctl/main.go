// gcctl is the CLI binary for Ground Control (Harbor Satellite).
//
// Usage:
//
//	gcctl [command] [flags]
//
// Build:
//
//	go build -o gcctl ./ground-control/cmd/gcctl
package main

import (
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/root"
)

func main() {
	if err := root.RootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
