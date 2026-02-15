// Package root defines the gcctl root command and wires all subcommands.
//
// Following the Harbor CLI pattern, each resource area (satellite, group, etc.)
// is a subcommand group with its own package. The root command sets up global
// flags (--config, --output, --server, --verbose) and Cobra groups for
// organized help output.
package root

import (

	// "os"

	"github.com/spf13/cobra"
)

var (
	// Global flags bound to persistent flags on the root command.
	cfgFile      string
	outputFormat string
	serverURL    string
	verbose      bool
)

// RootCmd creates and returns the root cobra command with all subcommands wired in.
func RootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gcctl",
		Short: "Ground Control CLI for Harbor Satellite",
		Long: `gcctl is a command-line tool for managing Harbor Satellite infrastructure.
It communicates with the Ground Control server to manage satellites,
groups, configs, and users.
To get started, run:
  gcctl login --server https://your-ground-control-server.com`,
		Example: `  # Login to Ground Control
  gcctl login --server https://gc.example.com
  # Show current user
  gcctl whoami
  # List satellites (future)
  gcctl satellite list`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global persistent flags (available to all subcommands)
	root.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is ~/.gcctl/config.yaml)")
	root.PersistentFlags().StringVarP(&outputFormat, "output", "o", "", "output format: table, json, yaml")
	root.PersistentFlags().StringVarP(&serverURL, "server", "s", "", "Ground Control server URL (overrides config)")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	return root
}
