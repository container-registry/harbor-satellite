// Package root defines the gcctl root command and wires all subcommands.
//
// Following the Harbor CLI pattern, each resource area (satellite, group, etc.)
// is a subcommand group with its own package. The root command sets up global
// flags (--config, --output, --server, --verbose) and Cobra groups for
// organized help output.
package root

import (
	"fmt"
	"os"

	gcctlconfig "github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/pkg/config"
	"github.com/spf13/cobra"
)

var (
	// Global flags bound to persistent flags on the root command.
	cfgFile      string
	outputFormat string
	serverURL    string
	verbose      bool
)
func GetConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	path, err := gcctlconfig.DefaultConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not determine default config path: %v\n", err)
		return ""
	}
	return path
}

// GetOutputFormat returns the output format from the --output flag.
func GetOutputFormat() string {
	return outputFormat
}

// GetServerURL returns the server URL from the --server flag.
// If not set, returns empty string (callers should fall back to config file).
func GetServerURL() string {
	return serverURL
}


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

	root.AddGroup(&cobra.Group{ID: "auth", Title: "Authentication:"})
	root.AddGroup(&cobra.Group{ID: "utils", Title: "Utility:"})

	loginCmd := LoginCommand()
	loginCmd.GroupID = "auth"
	root.AddCommand(loginCmd)

	logoutCmd := LogoutCommand()
	logoutCmd.GroupID = "auth"
	root.AddCommand(logoutCmd)

	whoamiCmd := WhoamiCommand()
	whoamiCmd.GroupID = "auth"
	root.AddCommand(whoamiCmd)

	versionCmd := VersionCommand()
	versionCmd.GroupID = "utils"
	root.AddCommand(versionCmd)

	return root
}

// LoadConfig is a helper used by subcommands to load the CLI config.
func LoadConfig() (*gcctlconfig.Config, error) {
	return gcctlconfig.Load(GetConfigPath())
}

// SaveConfig is a helper used by subcommands to persist the CLI config.
func SaveConfig(cfg *gcctlconfig.Config) error {
	return gcctlconfig.Save(GetConfigPath(), cfg)
}

// ResolveServer determines the Ground Control server URL.
// Priority: --server flag > config file > error.
func ResolveServer(cfg *gcctlconfig.Config) (string, error) {
	if s := GetServerURL(); s != "" {
		return s, nil
	}
	if cfg.Server != "" {
		return cfg.Server, nil
	}
	return "", fmt.Errorf("no server configured; use --server flag or run 'gcctl login'")
}