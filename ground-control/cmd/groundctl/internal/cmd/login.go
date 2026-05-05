package cmd

import (
	"fmt"
	"syscall"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/groundctl/internal/client"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLoginCmd(apiClient *client.Client) *cobra.Command {
	var username string
	var password string

	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Ground Control server",
		Long: `Login authenticates with the Ground Control server and stores the
session token locally for subsequent commands.

If --username or --password are not provided, you will be prompted
interactively.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if username == "" {
				fmt.Print("Username: ")
				if _, err := fmt.Scanln(&username); err != nil {
					return fmt.Errorf("read username: %w", err)
				}
			}

			if password == "" {
				fmt.Print("Password: ")
				passBytes, err := term.ReadPassword(int(syscall.Stdin))
				if err != nil {
					return fmt.Errorf("read password: %w", err)
				}
				fmt.Println()
				password = string(passBytes)
			}

			resp, err := apiClient.Login(username, password)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			cfg := &client.ConfigData{
				Server:    apiClient.Server(),
				Token:     resp.Token,
				ExpiresAt: resp.ExpiresAt,
			}

			if err := client.SaveConfig(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			fmt.Printf("Login successful. Token expires at %s\n", resp.ExpiresAt)
			return nil
		},
	}

	loginCmd.Flags().StringVarP(&username, "username", "u", "", "Ground Control username")
	loginCmd.Flags().StringVarP(&password, "password", "p", "", "Ground Control password")

	return loginCmd
}
