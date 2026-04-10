package root

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	gcctlconfig "github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/pkg/config"

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
func LoginCommand(opts *rootOpts) *cobra.Command {
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
			return runLogin(cmd, opts, username, password, passwordStdin)
		},
	}

	cmd.Flags().StringVarP(&username, "username", "u", "", "username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password (prefer --password-stdin)")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read password from stdin")

	return cmd
}

// loginCredentials holds the resolved server, username, and password.
type loginCredentials struct {
	server   string
	username string
	password string
}

// runLogin contains the core login logic, extracted to keep RunE within
// function-length and cyclomatic-complexity linter limits.
func runLogin(cmd *cobra.Command, opts *rootOpts, username, password string, passwordStdin bool) error {
	cfg, err := opts.loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	creds, err := gatherCredentials(cmd, opts, cfg, username, password, passwordStdin)
	if err != nil {
		return err
	}

	if err := api.ValidateScheme(creds.server); err != nil {
		return err
	}

	client := api.NewClient(creds.server, "")
	if err := client.Ping(cmd.Context()); err != nil {
		return fmt.Errorf("cannot reach server %s: %w", creds.server, err)
	}

	resp, err := client.Login(cmd.Context(), creds.username, creds.password)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	cfg.Server = creds.server
	cfg.Token = resp.Token
	cfg.ExpiresAt = resp.ExpiresAt
	cfg.Username = creds.username

	if err := opts.saveConfig(cfg); err != nil {
		return fmt.Errorf("login succeeded but failed to save config: %w", err)
	}

	fmt.Printf("Logged in as %s to %s\n", creds.username, creds.server)
	fmt.Printf("  Token expires: %s\n", resp.ExpiresAt)
	return nil
}

// gatherCredentials resolves server, username, and password from flags,
// config, or interactive prompts. A single bufio.Reader is shared across
// all prompt calls to avoid consuming buffered stdin bytes prematurely.
func gatherCredentials(cmd *cobra.Command, opts *rootOpts, cfg *gcctlconfig.Config, username, password string, passwordStdin bool) (*loginCredentials, error) {
	reader := bufio.NewReader(os.Stdin)

	// resolveServer checks --server flag first, then cfg.Server, then errors.
	server, err := opts.resolveServer(cfg)
	if err != nil {
		server, err = promptInput(reader, "Ground Control Server URL")
		if err != nil {
			return nil, err
		}
	}

	if username == "" {
		username, err = promptInput(reader, "Username")
		if err != nil {
			return nil, err
		}
	}

	if passwordStdin {
		password, err = readStdin(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read password from stdin: %w", err)
		}
	} else if password != "" {
		// Warn: -p exposes the password in shell history and `ps aux` output.
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning: --password/-p exposes credentials in shell history and process list. Use --password-stdin for scripts.")
	}

	if password == "" {
		password, err = promptPassword("Password")
		if err != nil {
			return nil, err
		}
	}

	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	return &loginCredentials{server: server, username: username, password: password}, nil
}

// promptInput reads a line of text from stdin with the given prompt label.
// Accepts a shared reader to avoid buffered-input loss across multiple prompts.
func promptInput(reader *bufio.Reader, label string) (string, error) {
	fmt.Printf("%s: ", label)
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
// Accepts a shared reader to avoid buffered-input loss across multiple prompts.
func readStdin(reader *bufio.Reader) (string, error) {
	input, err := reader.ReadString('\n')
	if err != nil {
		// EOF without newline is acceptable (e.g. echo -n "pass" | gcctl login)
		if len(input) > 0 {
			return strings.TrimSpace(input), nil
		}
		return "", err
	}
	return strings.TrimSpace(input), nil
}
