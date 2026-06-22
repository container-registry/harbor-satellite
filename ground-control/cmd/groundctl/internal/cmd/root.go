package cmd

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/groundctl/internal/client"
	"github.com/spf13/cobra"
)

// NewRootCommand creates the root cobra command for groundctl.
func NewRootCommand() *cobra.Command {
	apiClient := client.NewClient("http://localhost:8080")

	rootCmd := &cobra.Command{
		Use:   "groundctl",
		Short: "CLI for managing Harbor Satellite Ground Control",
		Long: `groundctl is a command-line interface for managing the Harbor Satellite
Ground Control server. It provides commands for satellite management,
group management, configuration, and more.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			serverFlag, err := cmd.Flags().GetString("server")
			if err != nil {
				return fmt.Errorf("get server flag: %w", err)
			}
			apiClient.SetServer(serverFlag)

			cfg, err := client.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if cfg.Token != "" {
				apiClient.SetToken(cfg.Token)
			}

			return nil
		},
	}

	rootCmd.PersistentFlags().String("server", "http://localhost:8080", "Ground Control server URL")

	rootCmd.AddCommand(newLoginCmd(apiClient))
	rootCmd.AddCommand(newSatelliteCmd(apiClient))

	return rootCmd
}
