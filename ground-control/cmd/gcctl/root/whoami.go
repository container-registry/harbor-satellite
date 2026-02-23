package root

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/pkg/api"
	"github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/pkg/utils"
	"github.com/spf13/cobra"
)

// WhoamiCommand returns the "whoami" cobra command.
//
// Whoami queries the Ground Control server for the current authenticated
// user's identity and displays their username, role, server, and
// token expiry. Supports table, json, and yaml output formats.
func WhoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current authenticated user",
		Long: `Display information about the currently logged-in user.
Shows the username, role, server URL, and token expiry from the
current session. Requires a prior 'gcctl login'.`,
		Example: `  gcctl whoami
  gcctl whoami -o json
  gcctl whoami -o yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if !cfg.HasCredentials() {
				return fmt.Errorf("not logged in; run 'gcctl login' first")
			}

			server, err := ResolveServer(cfg)
			if err != nil {
				return err
			}

			// Query the server for live user info
			client := api.NewClient(server, cfg.Token)
			resp, err := client.Whoami()
			if err != nil {
				return fmt.Errorf("failed to get user info: %w", err)
			}

			// Attach server and expiry from local config for display
			resp.Server = server

			// Build a display-friendly struct that includes the expiry
			display := whoamiDisplay{
				Username:  resp.Username,
				Role:      resp.Role,
				Server:    resp.Server,
				ExpiresAt: cfg.ExpiresAt,
			}

			format := GetOutputFormat()
			switch format {
			case "json", "yaml":
				return utils.PrintFormat(display, format)
			default:
				// Default table/key-value output
				utils.PrintKeyValue([][]string{
					{"Username", display.Username},
					{"Role", display.Role},
					{"Server", display.Server},
					{"Token Expires", display.ExpiresAt},
				})
				return nil
			}
		},
	}
}

// whoamiDisplay is the output structure for the whoami command.
// Exported fields with json/yaml tags so PrintFormat can serialize it.
type whoamiDisplay struct {
	Username  string `json:"username" yaml:"username"`
	Role      string `json:"role" yaml:"role"`
	Server    string `json:"server" yaml:"server"`
	ExpiresAt string `json:"expires_at" yaml:"expires_at"`
}
