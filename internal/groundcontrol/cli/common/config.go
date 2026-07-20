package common

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	defaultServerURL = "https://localhost:8080/"
	defaultTimeout   = 30 * time.Second

	urlKey             = "url"
	tokenKey           = "token"
	timeoutKey         = "timeout"
	insecureKey        = "insecure"
	credentialsFileKey = "credentials-file"

	credentialsDirName = "groundcontrol"
	credentialsFile    = "credentials.json"
)

type flags struct {
	configFile string
	serverURL  string
	token      string
	timeout    time.Duration
	insecure   bool
}

// Runtime owns the configuration and generated client used by CLI commands.
type Runtime struct {
	config *viper.Viper
	client *groundcontrol.ClientWithResponses
}

// Load initializes Viper, defines and binds the root flags, and installs the
// configuration loader that runs after Cobra has parsed command-line flags.
func Load(command *cobra.Command) *Runtime {
	rootFlags := flags{}
	configuration := viper.New()
	// configuration.SetEnvPrefix("GROUND_CONTROL")
	configuration.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	configuration.AutomaticEnv()
	configuration.SetDefault(urlKey, defaultServerURL)
	configuration.SetDefault(timeoutKey, defaultTimeout)
	configuration.SetDefault(insecureKey, false)
	configuration.SetDefault(credentialsFileKey, "")

	command.PersistentFlags().StringVar(&rootFlags.configFile, "config", "", "configuration file")
	command.PersistentFlags().StringVar(&rootFlags.serverURL, "server", defaultServerURL, "Ground Control server URL")
	command.PersistentFlags().StringVar(&rootFlags.token, "token", "", "Ground Control bearer token")
	command.PersistentFlags().DurationVar(&rootFlags.timeout, "timeout", defaultTimeout, "HTTP request timeout")
	command.PersistentFlags().BoolVar(&rootFlags.insecure, "insecure", false, "skip HTTPS certificate verification")

	mustBindFlag(configuration.BindPFlag(urlKey, command.PersistentFlags().Lookup("server")))
	mustBindFlag(configuration.BindPFlag(tokenKey, command.PersistentFlags().Lookup("token")))
	mustBindFlag(configuration.BindPFlag(timeoutKey, command.PersistentFlags().Lookup("timeout")))
	mustBindFlag(configuration.BindPFlag(insecureKey, command.PersistentFlags().Lookup("insecure")))

	runtime := &Runtime{config: configuration}
	command.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if rootFlags.configFile != "" {
			configuration.SetConfigFile(rootFlags.configFile)
			if err := configuration.ReadInConfig(); err != nil {
				return fmt.Errorf("read config file %q: %w", rootFlags.configFile, err)
			}
		}
		if err := runtime.loadStoredToken(); err != nil {
			return err
		}
		return runtime.Initialize()
	}
	return runtime
}

func (r *Runtime) loadStoredToken() error {
	if strings.TrimSpace(r.config.GetString(tokenKey)) != "" {
		return nil
	}

	credentials, _, err := r.loadCredentials()
	if err != nil {
		return err
	}
	sessionKey := sessionConfigKey(r.config.GetString(urlKey))
	token := strings.TrimSpace(credentials.GetString(sessionKey + ".token"))
	if token == "" {
		return nil
	}

	if expiration := credentials.GetString(sessionKey + ".expires_at"); expiration != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, expiration)
		if err != nil {
			return fmt.Errorf("decode stored token expiration: %w", err)
		}
		if !time.Now().Before(expiresAt) {
			return nil
		}
	}

	// A stored session is a fallback below flags, environment variables, and
	// configuration-file values in Viper's precedence order.
	r.config.SetDefault(tokenKey, token)
	return nil
}

func (r *Runtime) StoreToken(username string, token string, expiresAt time.Time) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("login response did not contain a token")
	}

	credentials, path, err := r.loadCredentials()
	if err != nil {
		return err
	}
	server := normalizeServerURL(r.config.GetString(urlKey))
	sessionKey := sessionConfigKey(server)
	credentials.Set(sessionKey+".server", server)
	credentials.Set(sessionKey+".username", username)
	credentials.Set(sessionKey+".token", token)
	credentials.Set(sessionKey+".expires_at", expiresAt.UTC().Format(time.RFC3339Nano))
	return writeCredentials(credentials, path)
}

func (r *Runtime) RemoveStoredToken() error {
	credentials, path, err := r.loadCredentials()
	if err != nil {
		return err
	}
	sessions := credentials.GetStringMap("sessions")
	delete(sessions, sessionID(r.config.GetString(urlKey)))
	credentials.Set("sessions", sessions)
	return writeCredentials(credentials, path)
}

func (r *Runtime) loadCredentials() (*viper.Viper, string, error) {
	path, err := r.credentialsPath()
	if err != nil {
		return nil, "", err
	}

	credentials := viper.New()
	credentials.SetConfigFile(path)
	credentials.SetConfigType("json")
	if err := credentials.ReadInConfig(); err != nil && !isConfigNotFound(err) {
		return nil, "", fmt.Errorf("read login credentials %q: %w", path, err)
	}
	return credentials, path, nil
}

func (r *Runtime) credentialsPath() (string, error) {
	if path := strings.TrimSpace(r.config.GetString(credentialsFileKey)); path != "" {
		return path, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(configDir, credentialsDirName, credentialsFile), nil
}

func writeCredentials(credentials *viper.Viper, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create login credentials directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open login credentials %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close login credentials %q: %w", path, err)
	}
	if err := credentials.WriteConfigAs(path); err != nil {
		return fmt.Errorf("write login credentials %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure login credentials %q: %w", path, err)
	}
	return nil
}

func isConfigNotFound(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	return errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist)
}

func sessionConfigKey(serverURL string) string {
	return "sessions." + sessionID(serverURL)
}

func sessionID(serverURL string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalizeServerURL(serverURL))))
}

func normalizeServerURL(serverURL string) string {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return strings.TrimRight(serverURL, "/")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

func mustBindFlag(err error) {
	if err != nil {
		panic(err)
	}
}
