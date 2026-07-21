package main

import (
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/groundctl/internal/cmd"
)

func main() {
	if err := cmd.NewRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
