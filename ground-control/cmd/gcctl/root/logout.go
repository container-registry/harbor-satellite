package root

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/pkg/api"
	"github.com/spf13/cobra"
)

// LogoutCommand returns the "logout" cobra command.
//
// Logout invalidates the current session on the Ground Control server
// and clears the token from the local config file.
// Even if the server call fails (e.g. expired token), the local
// credentials are still cleared.
func LogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Log out from Ground Control",
		Long:    `Invalidate the current session token and clear local credentials.`,
		Example: "  gcctl logout",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if !cfg.HasCredentials() {
				fmt.Println("Not logged in.")
				return nil
			}

			server, err := ResolveServer(cfg)
			if err != nil {
				// No server known — just clear local state
				cfg.ClearCredentials()
				_ = SaveConfig(cfg)
				fmt.Println("Local credentials cleared.")
				return nil
			}

			// Try to invalidate the session on the server
			client := api.NewClient(server, cfg.Token)
			if err := client.Logout(); err != nil {
				// Server call failed, but still clear local credentials
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: server logout failed: %v\n", err)
			}

			// Always clear local state
			cfg.ClearCredentials()
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Println("Logged out successfully.")
			return nil
		},
	}
}