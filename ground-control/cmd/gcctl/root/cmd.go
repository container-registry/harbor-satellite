// Package root defines the gcctl root command and wires all subcommands.
package root

import (
	"fmt"

	gcctlconfig "github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/pkg/config"
	"github.com/spf13/cobra"
)

// rootOpts holds global CLI flag values, scoped to the root command's lifetime.
// Keeping them here (rather than as package-level vars) satisfies gochecknoglobals
// and makes each command invocation own its own state.
type rootOpts struct {
	cfgFile      string
	outputFormat string
	serverURL    string
	verbose      bool
}

func (o *rootOpts) configPath() (string, error) {
	if o.cfgFile != "" {
		return o.cfgFile, nil
	}
	path, err := gcctlconfig.DefaultConfigPath()
	if err != nil {
		return "", fmt.Errorf("could not determine default config path: %w", err)
	}
	return path, nil
}

func (o *rootOpts) loadConfig() (*gcctlconfig.Config, error) {
	path, err := o.configPath()
	if err != nil {
		return nil, err
	}
	return gcctlconfig.Load(path)
}

func (o *rootOpts) saveConfig(cfg *gcctlconfig.Config) error {
	path, err := o.configPath()
	if err != nil {
		return err
	}
	return gcctlconfig.Save(path, cfg)
}

// resolveServer determines the Ground Control server URL.
// Priority: --server flag > config file > error.
func (o *rootOpts) resolveServer(cfg *gcctlconfig.Config) (string, error) {
	if o.serverURL != "" {
		return o.serverURL, nil
	}
	if cfg != nil && cfg.Server != "" {
		return cfg.Server, nil
	}
	return "", fmt.Errorf("no server configured; use --server flag or run 'gcctl login'")
}

// RootCmd creates and returns the root cobra command with all subcommands wired in.
func RootCmd() *cobra.Command {
	opts := &rootOpts{}

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
  gcctl whoami`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Persistent flags are bound to the opts struct, not package-level vars.
	root.PersistentFlags().StringVarP(&opts.cfgFile, "config", "c", "", "config file (default is ~/.gcctl/config.yaml)")
	root.PersistentFlags().StringVarP(&opts.outputFormat, "output", "o", "", "output format: table, json, yaml")
	root.PersistentFlags().StringVarP(&opts.serverURL, "server", "s", "", "Ground Control server URL (overrides config)")
	root.PersistentFlags().BoolVarP(&opts.verbose, "verbose", "v", false, "verbose output")

	root.AddGroup(&cobra.Group{ID: "auth", Title: "Authentication:"})
	root.AddGroup(&cobra.Group{ID: "utils", Title: "Utility:"})

	loginCmd := LoginCommand(opts)
	loginCmd.GroupID = "auth"
	root.AddCommand(loginCmd)

	logoutCmd := LogoutCommand(opts)
	logoutCmd.GroupID = "auth"
	root.AddCommand(logoutCmd)

	whoamiCmd := WhoamiCommand(opts)
	whoamiCmd.GroupID = "auth"
	root.AddCommand(whoamiCmd)

	versionCmd := VersionCommand()
	versionCmd.GroupID = "utils"
	root.AddCommand(versionCmd)

	return root
}
