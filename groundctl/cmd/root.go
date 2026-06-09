package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/groundctl/internal/client"
	"github.com/spf13/cobra"
)

// contextKey is used to store the client in the command context.
type contextKey string

const clientKey contextKey = "gc-client"

// gcURL and token are the persistent global flags.
var (
	gcURL  string
	token  string
	outFmt string
)

// rootCmd is the base command for groundctl.
var rootCmd = &cobra.Command{
	Use:   "groundctl",
	Short: "groundctl — CLI for managing Harbor Satellite fleets via Ground Control",
	Long: `groundctl is a command-line tool for managing Harbor Satellite fleets.

It communicates directly with the Ground Control API to register, inspect,
and delete satellites, manage groups and configs, and reconcile declarative
fleet manifests.

Environment variables:
  GROUNDCTL_URL    Ground Control base URL (overrides --gc-url)
  GROUNDCTL_TOKEN  Bearer token (overrides --token)`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Resolve URL from flag or env
		if gcURL == "" {
			gcURL = os.Getenv("GROUNDCTL_URL")
		}
		if gcURL == "" {
			return fmt.Errorf("Ground Control URL is required: set --gc-url or GROUNDCTL_URL")
		}

		// Resolve token from flag or env
		if token == "" {
			token = os.Getenv("GROUNDCTL_TOKEN")
		}

		// Store client in context so subcommands can retrieve it
		gc := client.New(gcURL, token)
		ctx := context.WithValue(cmd.Context(), clientKey, gc)
		cmd.SetContext(ctx)
		return nil
	},
}

// Execute is the entry point called from main.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&gcURL, "gc-url", "", "Ground Control base URL (e.g. http://ground-control:8080)")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "Bearer token for authentication")
	rootCmd.PersistentFlags().StringVarP(&outFmt, "output", "o", "table", "Output format: table or json")

	rootCmd.AddCommand(
		newSatelliteCmd(),
		newGroupCmd(),
		newConfigCmd(),
		newApplyCmd(),
		newVersionCmd(),
	)
}

// clientFromContext retrieves the Ground Control client stored in command context.
func clientFromContext(cmd *cobra.Command) *client.Client {
	gc, ok := cmd.Context().Value(clientKey).(*client.Client)
	if !ok || gc == nil {
		fmt.Fprintln(os.Stderr, "internal error: client not initialised")
		os.Exit(1)
	}
	return gc
}
