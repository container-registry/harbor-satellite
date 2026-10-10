package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the current groundctl version.
// Set at build time via: -ldflags "-X github.com/container-registry/harbor-satellite/groundctl/cmd.Version=v0.1.0"
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the groundctl version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("groundctl %s\n", Version)
		},
	}
}
