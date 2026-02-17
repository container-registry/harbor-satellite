package root

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/pkg/api"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// LoginCommand returns the "login" cobra command.
//
// Login authenticates with a Ground Control server, obtains a session token,
// and stores it in the CLI config file (~/.gcctl/config.yaml).
//
// The server URL can be provided via:
//  1. --server flag (highest priority)
//  2. Config file (if previously saved)
//  3. Interactive prompt (if neither is available)
//
// Username and password can be provided via flags or interactive prompt.
// For scripting, use --password-stdin to pipe the password securely.
func LoginCommand() *cobra.Command {
	var (
		username      string
		password      string
		passwordStdin bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Ground Control",
		Long: `Authenticate with a Ground Control server and store the session token.
The token is saved in ~/.gcctl/config.yaml and used for subsequent commands.
If --server is not provided, the value from the config file is used.`,
		Example: `  # Interactive login (prompts for all fields)
  gcctl login --server https://gc.example.com
  # Non-interactive login
  gcctl login --server https://gc.example.com -u admin -p secret
  # Secure password via stdin (for scripts)
  echo "secret" | gcctl login --server https://gc.example.com -u admin --password-stdin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Resolve server URL
			server, err := ResolveServer(cfg)
			if err != nil {
				// Neither flag nor config has a server; prompt interactively
				server, err = promptInput("Ground Control Server URL")
				if err != nil {
					return err
				}
			}

			// Prompt for username if not provided via flag
			if username == "" {
				username, err = promptInput("Username")
				if err != nil {
					return err
				}
			}

			// Read password from stdin if --password-stdin is set
			if passwordStdin {
				password, err = readStdin()
				if err != nil {
					return fmt.Errorf("failed to read password from stdin: %w", err)
				}
			}

			// Prompt for password if still empty
			if password == "" {
				password, err = promptPassword("Password")
				if err != nil {
					return err
				}
			}

			if username == "" || password == "" {
				return fmt.Errorf("username and password are required")
			}

			// Ping the server first to check connectivity
			client := api.NewClient(server, "")
			if err := client.Ping(); err != nil {
				return fmt.Errorf("cannot reach server %s: %w", server, err)
			}

			// Authenticate
			resp, err := client.Login(username, password)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			// Save credentials to config file
			cfg.Server = server
			cfg.Token = resp.Token
			cfg.ExpiresAt = resp.ExpiresAt
			cfg.Username = username

			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("login succeeded but failed to save config: %w", err)
			}

			fmt.Printf("✓ Logged in as %s to %s\n", username, server)
			fmt.Printf("  Token expires: %s\n", resp.ExpiresAt)
			return nil
		},
	}

	cmd.Flags().StringVarP(&username, "username", "u", "", "username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password (prefer --password-stdin)")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read password from stdin")

	return cmd
}

// promptInput reads a line of text from stdin with the given prompt label.
func promptInput(label string) (string, error) {
	fmt.Printf("%s: ", label)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimSpace(input), nil
}

// promptPassword reads a password from the terminal without echoing.
func promptPassword(label string) (string, error) {
	fmt.Printf("%s: ", label)
	passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // newline after hidden input
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return strings.TrimSpace(string(passBytes)), nil
}

// readStdin reads the first line from stdin (for --password-stdin).
func readStdin() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		// If EOF without newline, that's okay (e.g. echo -n "pass" | gcctl login)
		if len(input) > 0 {
			return strings.TrimSpace(input), nil
		}
		return "", err
	}
	return strings.TrimSpace(input), nil
}