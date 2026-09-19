package cli

import (
	"errors"
	"fmt"
	"strings"

	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// mirrorFlags is a repeatable CRI-to-registry mapping flag.
type mirrorFlags []string

func (m *mirrorFlags) String() string {
	return fmt.Sprint(*m)
}

func (m *mirrorFlags) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func (*mirrorFlags) Type() string {
	return "mirror"
}

type proxyModeFlag struct {
	mode *proxy.Mode
}

func (f proxyModeFlag) String() string {
	return f.mode.String()
}

func (f proxyModeFlag) Set(value string) error {
	return f.mode.Set(value)
}

func (proxyModeFlag) Type() string {
	return "mode"
}

// SatelliteOptions holds CLI flag and environment configuration.
type SatelliteOptions struct {
	JSONLogging            bool
	GroundControlURL       string
	Token                  string
	UseUnsecure            bool
	Mirrors                mirrorFlags
	SPIFFEEnabled          bool
	SPIFFEEndpointSocket   string
	SPIFFEExpectedServerID string
	BYORegistry            bool
	RegistryURL            string
	RegistryUsername       string
	RegistryPassword       string
	ConfigDir              string
	RegistryDataDir        string
	NoRegistryFallback     bool
	HarborRegistryURL      string
	DirectDelivery         bool
	ImageDir               string
	ProxyMode              proxy.Mode
	ProxyPort              int
	ShutdownTimeout        string
	StateURL               string
	StateAuthURL           string
	StateAuthUsername      string
	StateAuthPassword      string

	envToken             string
	envRegistryPassword  string
	envStateAuthPassword string
}

// RootCmd creates the Satellite command tree. Running it without a subcommand
// prints help and performs no configuration or service startup.
func RootCmd() *cobra.Command {
	opts, envErr := optionsFromEnvironment()
	rootCmd := &cobra.Command{
		Use:           "satellite",
		Short:         "Run and configure Harbor Satellite",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	rootCmd.AddCommand(
		newServeCommand(opts, envErr),
		newConfigureCommand(opts, envErr),
	)

	return rootCmd
}

func optionsFromEnvironment() (*SatelliteOptions, error) {
	_ = godotenv.Load(".env") //nolint:errcheck // .env is optional.

	opts := &SatelliteOptions{
		JSONLogging:          false,
		ProxyMode:            proxy.ModeProxy,
		ProxyPort:            config.DefaultProxyPort,
		SPIFFEEndpointSocket: config.DefaultSPIFFEEndpointSocket,
	}
	if err := env.LoadSatellite(); err != nil {
		return opts, fmt.Errorf("invalid environment: %w", err)
	}

	envCfg := env.Satellite.ApplyDefaults()
	opts.GroundControlURL = envCfg.GroundControlURL
	opts.UseUnsecure = envCfg.UseUnsecure
	opts.SPIFFEEnabled = envCfg.SPIFFEEnabled
	opts.SPIFFEEndpointSocket = envCfg.SPIFFEEndpointSocket
	opts.SPIFFEExpectedServerID = envCfg.SPIFFEExpectedServerID
	opts.BYORegistry = envCfg.BYORegistry
	opts.RegistryURL = envCfg.RegistryURL
	opts.RegistryUsername = envCfg.RegistryUsername
	opts.ConfigDir = envCfg.ConfigDir
	opts.RegistryDataDir = envCfg.RegistryDataDir
	opts.NoRegistryFallback = envCfg.NoRegistryFallback
	opts.HarborRegistryURL = envCfg.HarborRegistryURL
	opts.DirectDelivery = envCfg.DirectDelivery
	opts.ImageDir = envCfg.ImageDir
	opts.ProxyMode = envCfg.ProxyMode
	opts.ProxyPort = envCfg.ProxyPort
	opts.ShutdownTimeout = envCfg.ShutdownTimeout
	opts.StateURL = envCfg.StateURL
	opts.StateAuthURL = envCfg.StateAuthURL
	opts.StateAuthUsername = envCfg.StateAuthUsername
	opts.envToken = envCfg.Token
	opts.envRegistryPassword = envCfg.RegistryPassword
	opts.envStateAuthPassword = envCfg.StateAuthPassword

	return opts, nil
}

func newServeCommand(opts *SatelliteOptions, envErr error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Satellite service and OCI proxy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if envErr != nil {
				return envErr
			}
			opts.applySecretEnvironment()
			pathConfig, cm, warnings, err := initializeConfig(opts, true)
			if err != nil {
				return err
			}
			return run(cmd.Context(), opts, pathConfig, cm, warnings)
		},
	}
	bindConfigurationFlags(cmd, opts)
	bindServeFlags(cmd, opts)
	return cmd
}

func newConfigureCommand(opts *SatelliteOptions, envErr error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure container runtimes and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if envErr != nil {
				return envErr
			}
			opts.applySecretEnvironment()
			_, cm, _, err := initializeConfig(opts, false)
			if err != nil {
				return err
			}
			results := resolveCRIAndApply(
				cm,
				opts.Mirrors,
				opts.NoRegistryFallback,
				resolveLocalRegistryEndpoint(opts.ProxyMode, opts.ProxyPort),
			)
			printCRIResults(cmd, results)
			fmt.Fprintln(cmd.OutOrStdout(), "Container runtime configuration complete.")
			return nil
		},
	}
	bindConfigurationFlags(cmd, opts)
	return cmd
}

func bindConfigurationFlags(cmd *cobra.Command, opts *SatelliteOptions) {
	flags := cmd.Flags()
	flags.StringVar(&opts.ConfigDir, "config-dir", opts.ConfigDir, "Directory containing config.json")
	flags.BoolVar(&opts.UseUnsecure, "use-unsecure", opts.UseUnsecure, "Use insecure registry connections")
	flags.Var(&opts.Mirrors, "mirrors", "Override CRI registry config (CRI:registry1,registry2)")
	flags.BoolVar(&opts.NoRegistryFallback, "no-registry-fallback", opts.NoRegistryFallback, "Disable CRI registry fallback configuration")
	flags.Var(proxyModeFlag{mode: &opts.ProxyMode}, "proxy-mode", "OCI proxy mode: proxy or replica")
	flags.IntVar(&opts.ProxyPort, "proxy-port", opts.ProxyPort, "Local OCI registry proxy port")
}

func bindServeFlags(cmd *cobra.Command, opts *SatelliteOptions) {
	flags := cmd.Flags()
	flags.StringVar(&opts.GroundControlURL, "ground-control-url", opts.GroundControlURL, "Optional Ground Control URL")
	flags.StringVar(&opts.Token, "token", "", "Satellite bootstrap token")
	flags.StringVar(&opts.StateURL, "state-url", opts.StateURL, "Satellite state artifact URL")
	flags.StringVar(&opts.StateAuthURL, "state-auth-url", opts.StateAuthURL, "Upstream Harbor registry URL")
	flags.StringVar(&opts.StateAuthUsername, "state-auth-username", opts.StateAuthUsername, "Upstream Harbor registry username")
	flags.StringVar(&opts.StateAuthPassword, "state-auth-password", "", "Upstream Harbor registry password")
	flags.BoolVar(&opts.JSONLogging, "json-logging", opts.JSONLogging, "Enable JSON logging")
	flags.BoolVar(&opts.SPIFFEEnabled, "spiffe-enabled", opts.SPIFFEEnabled, "Enable SPIFFE/SPIRE authentication")
	flags.StringVar(&opts.SPIFFEEndpointSocket, "spiffe-endpoint-socket", opts.SPIFFEEndpointSocket, "SPIFFE Workload API endpoint socket")
	flags.StringVar(&opts.SPIFFEExpectedServerID, "spiffe-expected-server-id", opts.SPIFFEExpectedServerID, "Expected SPIFFE ID of Ground Control")
	flags.BoolVar(&opts.BYORegistry, "byo-registry", opts.BYORegistry, "Use an external registry as the replication store")
	flags.StringVar(&opts.RegistryURL, "registry-url", opts.RegistryURL, "External replication registry URL")
	flags.StringVar(&opts.RegistryUsername, "registry-username", opts.RegistryUsername, "External replication registry username")
	flags.StringVar(&opts.RegistryPassword, "registry-password", "", "External replication registry password")
	flags.StringVar(&opts.RegistryDataDir, "registry-data-dir", opts.RegistryDataDir, "Registry data directory")
	flags.StringVar(&opts.ShutdownTimeout, "shutdown-timeout", opts.ShutdownTimeout, "Graceful shutdown timeout")
	flags.StringVar(&opts.HarborRegistryURL, "harbor-registry-url", opts.HarborRegistryURL, "Override the upstream Harbor registry URL")
	flags.BoolVar(&opts.DirectDelivery, "direct-delivery", opts.DirectDelivery, "Write image tarballs directly to a k3s/RKE2 image directory")
	flags.StringVar(&opts.ImageDir, "image-dir", opts.ImageDir, "Image directory for direct delivery")
}

func (opts *SatelliteOptions) applySecretEnvironment() {
	if opts.Token == "" {
		opts.Token = opts.envToken
	}
	if opts.RegistryPassword == "" {
		opts.RegistryPassword = opts.envRegistryPassword
	}
	if opts.StateAuthPassword == "" {
		opts.StateAuthPassword = opts.envStateAuthPassword
	}
}

func validateSatelliteOptions(opts *SatelliteOptions) error {
	if !opts.ProxyMode.Valid() {
		return fmt.Errorf("--proxy-mode must be %q or %q, got %q", proxy.ModeProxy, proxy.ModeReplica, opts.ProxyMode)
	}
	if opts.ProxyPort < 1 || opts.ProxyPort > 65535 {
		return fmt.Errorf("--proxy-port must be between 1 and 65535, got %d", opts.ProxyPort)
	}
	if opts.BYORegistry && strings.TrimSpace(opts.RegistryURL) == "" {
		return errors.New("--registry-url is required when --byo-registry is enabled")
	}
	return nil
}

func initializeConfig(opts *SatelliteOptions, requireServeInputs bool) (*config.PathConfig, *config.ConfigManager, []string, error) {
	if err := validateSatelliteOptions(opts); err != nil {
		return nil, nil, nil, err
	}

	configDirProvided := strings.TrimSpace(opts.ConfigDir) != ""
	if requireServeInputs && !configDirProvided && !opts.hasCompleteStateFlags() && !opts.canBootstrap(opts.GroundControlURL) {
		return nil, nil, nil, errors.New("--config-dir is required unless complete state auth flags or Ground Control bootstrap credentials are provided")
	}

	if !configDirProvided {
		defaultDir, err := config.DefaultConfigDir()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("resolve default config directory: %w", err)
		}
		opts.ConfigDir = defaultDir
	}
	pathConfig, err := config.ResolvePathConfig(opts.ConfigDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve config paths: %w", err)
	}
	if opts.RegistryDataDir != "" {
		pathConfig.StoreDir = opts.RegistryDataDir
	}

	cm, warnings, err := config.InitConfigManager(
		opts.Token,
		opts.GroundControlURL,
		pathConfig.ConfigFile,
		pathConfig.PrevConfigFile,
		opts.JSONLogging,
		opts.UseUnsecure,
	)
	if err != nil {
		return nil, nil, warnings, fmt.Errorf("initialize config manager: %w", err)
	}
	if err := applyConfigOverrides(cm, opts); err != nil {
		return nil, nil, warnings, err
	}

	if requireServeInputs {
		stateConfig := cm.GetStateConfig()
		stateErr := config.ValidateStateConfig(stateConfig)
		switch {
		case stateErr == nil:
		case config.IsStateConfigEmpty(stateConfig) && opts.canBootstrapConfig(cm):
		default:
			return nil, nil, warnings, fmt.Errorf("invalid state_config: %w", stateErr)
		}
	}

	return pathConfig, cm, warnings, nil
}

func applyConfigOverrides(cm *config.ConfigManager, opts *SatelliteOptions) error {
	cm.With(func(cfg *config.Config) {
		if opts.StateURL != "" {
			cfg.StateConfig.StateURL = opts.StateURL
		}
		if opts.StateAuthURL != "" {
			cfg.StateConfig.RegistryCredentials.URL = config.URL(opts.StateAuthURL)
		}
		if opts.StateAuthUsername != "" {
			cfg.StateConfig.RegistryCredentials.Username = opts.StateAuthUsername
		}
		if opts.StateAuthPassword != "" {
			cfg.StateConfig.RegistryCredentials.Password = opts.StateAuthPassword
		}
	})

	if opts.SPIFFEEnabled {
		cm.With(config.SetSPIFFEConfig(config.SPIFFEConfig{
			Enabled:          true,
			EndpointSocket:   opts.SPIFFEEndpointSocket,
			ExpectedServerID: opts.SPIFFEExpectedServerID,
		}))
	}
	if opts.BYORegistry {
		cm.With(
			config.SetBringOwnRegistry(true),
			config.SetLocalRegistryURL(opts.RegistryURL),
			config.SetLocalRegistryUsername(opts.RegistryUsername),
			config.SetLocalRegistryPassword(opts.RegistryPassword),
		)
	}
	if opts.HarborRegistryURL == "" {
		return nil
	}

	cm.With(config.SetHarborRegistryURL(opts.HarborRegistryURL))
	if !cm.IsZTRDone() {
		return nil
	}
	stateConfig, err := config.ApplyHarborRegistryOverride(cm.GetStateConfig(), opts.HarborRegistryURL)
	if err != nil {
		return fmt.Errorf("apply Harbor registry URL override: %w", err)
	}
	cm.With(config.SetStateConfig(stateConfig))
	return nil
}

func (opts *SatelliteOptions) hasCompleteStateFlags() bool {
	return strings.TrimSpace(opts.StateURL) != "" &&
		strings.TrimSpace(opts.StateAuthURL) != "" &&
		strings.TrimSpace(opts.StateAuthUsername) != "" &&
		strings.TrimSpace(opts.StateAuthPassword) != ""
}

func (opts *SatelliteOptions) canBootstrap(groundControlURL string) bool {
	if strings.TrimSpace(groundControlURL) == "" {
		return false
	}
	return opts.SPIFFEEnabled || strings.TrimSpace(opts.Token) != ""
}

func (opts *SatelliteOptions) canBootstrapConfig(cm *config.ConfigManager) bool {
	return cm.HasGroundControl() && (cm.IsSPIFFEEnabled() || strings.TrimSpace(opts.Token) != "")
}

func printCRIResults(cmd *cobra.Command, results []runtime.CRIConfigResult) {
	for _, result := range results {
		if result.Success {
			fmt.Fprintf(cmd.OutOrStdout(), "CRI %s configured (backup: %s)\n", result.CRI, result.BackupPath)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s config error: %s\n", result.CRI, result.Error)
		}
	}
}
