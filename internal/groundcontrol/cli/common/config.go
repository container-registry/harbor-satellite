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
	"github.com/joho/godotenv"
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
	credentialsKey     = "sessions"
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
	config           *viper.Viper
	client           *groundcontrol.ClientWithResponses
	usingStoredToken bool
}

type credentialStore struct {
	Sessions map[string]storedSession `json:"sessions" mapstructure:"sessions"`
}

type storedSession struct {
	Server    string `json:"server"             mapstructure:"server"`
	Username  string `json:"username,omitempty" mapstructure:"username"`
	Token     string `json:"token"              mapstructure:"token"`
	ExpiresAt string `json:"expires_at"         mapstructure:"expires_at"`
}

type credentialsConfig struct {
	path          string
	configuration *viper.Viper
	store         credentialStore
}

// Load initializes Viper, defines and binds the root flags, and installs the
// configuration loader that runs after Cobra has parsed command-line flags.
func Load(command *cobra.Command) *Runtime {
	_ = godotenv.Load() //nolint:errcheck // .env file is optional

	rootFlags := flags{}
	configuration := viper.New()
	configuration.SetEnvPrefix("GROUND_CONTROL")
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
	r.usingStoredToken = false
	if strings.TrimSpace(r.config.GetString(tokenKey)) != "" {
		return nil
	}

	credentials, err := r.loadCredentials()
	if err != nil {
		return err
	}
	server := normalizeServerURL(r.config.GetString(urlKey))
	sessionID := sessionID(server)
	session, exists := credentials.store.Sessions[sessionID]
	if !exists {
		return nil
	}

	token, valid := session.valid(server, time.Now())
	if !valid {
		delete(credentials.store.Sessions, sessionID)
		return credentials.save()
	}

	// A stored session is a fallback below flags, environment variables, and
	// configuration-file values in Viper's precedence order.
	r.config.SetDefault(tokenKey, token)
	r.usingStoredToken = true
	return nil
}

// UsingStoredToken reports whether the active client token came from the saved session.
func (r *Runtime) UsingStoredToken() bool {
	return r.usingStoredToken
}

func (r *Runtime) StoreToken(username string, token string, expiresAt time.Time) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("login response did not contain a token")
	}

	credentials, err := r.loadCredentials()
	if err != nil {
		return err
	}
	server := normalizeServerURL(r.config.GetString(urlKey))
	credentials.store.Sessions[sessionID(server)] = storedSession{
		Server:    server,
		Username:  username,
		Token:     token,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano),
	}
	return credentials.save()
}

func (r *Runtime) RemoveStoredToken() error {
	credentials, err := r.loadCredentials()
	if err != nil {
		return err
	}
	delete(credentials.store.Sessions, sessionID(r.config.GetString(urlKey)))
	if err := credentials.save(); err != nil {
		return err
	}
	r.usingStoredToken = false
	return nil
}

func (r *Runtime) loadCredentials() (*credentialsConfig, error) {
	path, err := r.credentialsPath()
	if err != nil {
		return nil, err
	}

	configuration := viper.New()
	configuration.SetConfigFile(path)
	configuration.SetConfigType("json")
	if err := configuration.ReadInConfig(); err != nil && !isConfigNotFound(err) {
		return nil, fmt.Errorf("read login credentials %q: %w", path, err)
	}

	store := credentialStore{Sessions: make(map[string]storedSession)}
	if err := configuration.Unmarshal(&store); err != nil {
		return nil, fmt.Errorf("decode login credentials %q: %w", path, err)
	}
	if store.Sessions == nil {
		store.Sessions = make(map[string]storedSession)
	}
	return &credentialsConfig{path: path, configuration: configuration, store: store}, nil
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

func (c *credentialsConfig) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create login credentials directory: %w", err)
	}
	c.configuration.Set(credentialsKey, c.store.Sessions)
	c.configuration.SetConfigPermissions(0o600)
	if err := c.configuration.WriteConfigAs(c.path); err != nil {
		return fmt.Errorf("write login credentials %q: %w", c.path, err)
	}
	return nil
}

func (s storedSession) valid(server string, now time.Time) (string, bool) {
	token := strings.TrimSpace(s.Token)
	if token == "" || normalizeServerURL(s.Server) != server || s.ExpiresAt == "" {
		return "", false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, s.ExpiresAt)
	if err != nil || !now.Before(expiresAt) {
		return "", false
	}
	return token, true
}

func isConfigNotFound(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	return errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist)
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
