package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	runtime "github.com/container-registry/harbor-satellite/internal/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/hotreload"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/registry"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/internal/watcher"
	"github.com/container-registry/harbor-satellite/pkg/config"

	_ "github.com/joho/godotenv/autoload"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

// mirrorFlags is a custom flag type for cri and mirror mappings
type mirrorFlags []string

// String implements flag.Value for mirrorFlags
func (m *mirrorFlags) String() string {
	return fmt.Sprint(*m)
}

func (m *mirrorFlags) Set(value string) error {
	*m = append(*m, value)
	return nil
}

// SatelliteOptions holds all CLI flag and env var configuration.
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
}

func main() {
	var opts SatelliteOptions
	var shutdownTimeout string

	flag.StringVar(&opts.GroundControlURL, "ground-control-url", "", "URL to ground control")
	flag.BoolVar(&opts.JSONLogging, "json-logging", true, "Enable JSON logging")
	flag.StringVar(&opts.Token, "token", "", "Satellite token")
	flag.BoolVar(&opts.UseUnsecure, "use-unsecure", false, "Use insecure (HTTP) connections to registries")
	flag.Var(&opts.Mirrors, "mirrors", "Specify CRI and registries in the form CRI:registry1,registry2")
	flag.BoolVar(&opts.SPIFFEEnabled, "spiffe-enabled", false, "Enable SPIFFE/SPIRE authentication")
	flag.StringVar(&opts.SPIFFEEndpointSocket, "spiffe-endpoint-socket", config.DefaultSPIFFEEndpointSocket, "SPIFFE Workload API endpoint socket")
	flag.StringVar(&opts.SPIFFEExpectedServerID, "spiffe-expected-server-id", "", "Expected SPIFFE ID of Ground Control server")
	flag.BoolVar(&opts.BYORegistry, "byo-registry", false, "Use external registry instead of embedded Zot")
	flag.StringVar(&opts.RegistryURL, "registry-url", "", "External registry URL")
	flag.StringVar(&opts.RegistryUsername, "registry-username", "", "External registry username")
	flag.StringVar(&opts.RegistryPassword, "registry-password", "", "External registry password")
	flag.StringVar(&opts.ConfigDir, "config-dir", "", "Configuration directory path (default: ~/.config/satellite)")
	flag.StringVar(&opts.RegistryDataDir, "registry-data-dir", "", "Registry data directory (overrides default storage path derived from config-dir)")
	flag.StringVar(&shutdownTimeout, "shutdown-timeout", "", "Graceful shutdown timeout (e.g., '30s'). Defaults to SHUTDOWN_TIMEOUT env var or 30s")

	flag.Parse()

	if opts.Token == "" {
		opts.Token = os.Getenv("TOKEN")
	}
	if opts.GroundControlURL == "" {
		opts.GroundControlURL = os.Getenv("GROUND_CONTROL_URL")
	}
	if os.Getenv("SPIFFE_ENABLED") == "true" {
		opts.SPIFFEEnabled = true
	}
	if os.Getenv("SPIFFE_ENDPOINT_SOCKET") != "" {
		opts.SPIFFEEndpointSocket = os.Getenv("SPIFFE_ENDPOINT_SOCKET")
	}
	if opts.SPIFFEExpectedServerID == "" && os.Getenv("SPIFFE_EXPECTED_SERVER_ID") != "" {
		opts.SPIFFEExpectedServerID = os.Getenv("SPIFFE_EXPECTED_SERVER_ID")
	}
	if !opts.UseUnsecure {
		opts.UseUnsecure = os.Getenv("USE_UNSECURE") == "true"
	}
	if !opts.BYORegistry {
		opts.BYORegistry = os.Getenv("BYO_REGISTRY") == "true"
	}
	if opts.RegistryURL == "" {
		opts.RegistryURL = os.Getenv("REGISTRY_URL")
	}
	if opts.RegistryUsername == "" {
		opts.RegistryUsername = os.Getenv("REGISTRY_USERNAME")
	}
	if opts.RegistryPassword == "" {
		opts.RegistryPassword = os.Getenv("REGISTRY_PASSWORD")
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = os.Getenv("CONFIG_DIR")
	}
	if opts.RegistryDataDir == "" {
		opts.RegistryDataDir = os.Getenv(config.RegistryDataDirEnvVar)
	}
	if shutdownTimeout == "" {
		shutdownTimeout = os.Getenv("SHUTDOWN_TIMEOUT")
		if shutdownTimeout == "" {
			shutdownTimeout = "30s"
		}
	}

	// Resolve config directory path
	if opts.ConfigDir == "" {
		var err error
		opts.ConfigDir, err = config.DefaultConfigDir()
		if err != nil {
			fmt.Printf("Error resolving default config directory: %v\n", err)
			os.Exit(1)
		}
	}

	pathConfig, err := config.ResolvePathConfig(opts.ConfigDir)
	if err != nil {
		fmt.Printf("Error resolving config paths: %v\n", err)
		os.Exit(1)
	}

	// Override ZotStorageDir if --registry-data-dir flag or env var is set
	if opts.RegistryDataDir != "" {
		pathConfig.ZotStorageDir = opts.RegistryDataDir
	}

	// Token is not required if SPIFFE is enabled
	if !opts.SPIFFEEnabled && (opts.Token == "" || opts.GroundControlURL == "") {
		fmt.Println("Missing required arguments: --token and --ground-control-url or matching env vars (or enable SPIFFE with --spiffe-enabled).")
		os.Exit(1)
	}
	if opts.GroundControlURL == "" {
		fmt.Println("Missing required argument: --ground-control-url or GROUND_CONTROL_URL env var.")
		os.Exit(1)
	}
	if opts.BYORegistry && opts.RegistryURL == "" {
		fmt.Println("Missing required argument: --registry-url is required when --byo-registry is enabled.")
		os.Exit(1)
	}

	err = run(opts, pathConfig, shutdownTimeout)
	if err != nil {
		fmt.Printf("fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(opts SatelliteOptions, pathConfig *config.PathConfig, shutdownTimeout string) error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()
	wg, ctx := errgroup.WithContext(ctx)

	cm, warnings, err := config.InitConfigManager(opts.Token, opts.GroundControlURL, pathConfig.ConfigFile, pathConfig.PrevConfigFile, opts.JSONLogging, opts.UseUnsecure)
	if err != nil {
		fmt.Printf("Error initiating the config manager: %v\n", err)
		return err
	}

	// Apply SPIFFE config from CLI flags
	if opts.SPIFFEEnabled {
		cm.With(config.SetSPIFFEConfig(config.SPIFFEConfig{
			Enabled:          opts.SPIFFEEnabled,
			EndpointSocket:   opts.SPIFFEEndpointSocket,
			ExpectedServerID: opts.SPIFFEExpectedServerID,
		}))
	}

	// Apply BYO registry config from CLI flags / env vars
	if opts.BYORegistry {
		cm.With(
			config.SetBringOwnRegistry(true),
			config.SetLocalRegistryURL(opts.RegistryURL),
			config.SetLocalRegistryUsername(opts.RegistryUsername),
			config.SetLocalRegistryPassword(opts.RegistryPassword),
		)
	}

	// Update Zot config with storage path
	zotConfigJSON, err := config.BuildZotConfigWithStoragePath(pathConfig.ZotStorageDir)
	if err != nil {
		return fmt.Errorf("build Zot config: %w", err)
	}
	cm.With(config.SetZotConfigRaw(json.RawMessage(zotConfigJSON)))

	// Resolve local registry endpoint for CRI mirror config
	localRegistryEndpoint, err := resolveLocalRegistryEndpoint(cm)
	if err != nil {
		return fmt.Errorf("resolving local registry endpoint: %w", err)
	}
	if err := runtime.ApplyCRIConfigs(opts.Mirrors, localRegistryEndpoint); err != nil {
		return fmt.Errorf("applying CRI configs: %w", err)
	}

	ctx, log := logger.InitLogger(ctx, cm.GetLogLevel(), opts.JSONLogging, warnings)

	// Write the config to disk, in case any defaults were enforced at runtime
	if err := cm.WriteConfig(); err != nil {
		log.Error().Err(err).Msg("Error writing config to disk")
		return err
	}

	hotReloadManager := hotreload.NewHotReloadManager(
		ctx,
		cm,
		log,
		pathConfig.ZotTempConfig,
		nil, // Will be set after scheduler creation
	)

	eventChan := make(chan struct{})

	// Handle registry setup
	wg.Go(func() error { return handleRegistrySetup(ctx, log, cm, pathConfig) })

	// Watch for changes in the config file
	wg.Go(func() error {
		return watcher.WatchChanges(ctx, log.With().Str("component", "file watcher").Logger(), pathConfig.ConfigFile, eventChan)
	})

	// Watch for changes in the config file
	wg.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-eventChan:
				log.Info().Msg("Event chan event received")
				changes, warnings, err := cm.ReloadConfig()
				if err != nil {
					log.Error().Err(err).Msg("Failed to reload configuration")
				} else {
					if len(warnings) > 0 {
						for _, warning := range warnings {
							log.Warn().Str("warning", warning).Msg("Configuration reload warning")
						}
					}
					if len(changes) > 0 {
						if err := hotReloadManager.ProcessConfigChanges(changes); err != nil {
							log.Error().Err(err).Msg("Error processing configuration changes")
						}
					}
				}
			}
		}
	})

	s := satellite.NewSatellite(cm, pathConfig.StateFile)
	err = s.Run(ctx)
	if err != nil {
		return fmt.Errorf("unable to start satellite: %w", err)
	}

	for _, s := range s.GetSchedulers() {
		if s.Name() == config.ReplicateStateJobName {
			hotReloadManager.SetStateReplicationScheduler(s)
		}
	}

	return gracefulShutdown(ctx, log, s, wg, shutdownTimeout)
}

func gracefulShutdown(ctx context.Context, log *zerolog.Logger, s *satellite.Satellite, wg *errgroup.Group, shutdownTimeout string) error {
	// Wait until context is cancelled
	<-ctx.Done()

	// Graceful shutdown with timeout
	shutdownDuration, err := time.ParseDuration(shutdownTimeout)
	if err != nil {
		log.Warn().Err(err).Str("shutdownTimeout", shutdownTimeout).
			Msg("Invalid shutdown timeout, defaulting to 30s")
		shutdownDuration = 30 * time.Second
	}

	log.Info().Dur("timeout", shutdownDuration).
		Msg("Received shutdown signal, initiating graceful shutdown")

	// Create a shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownDuration)
	defer shutdownCancel()

	// Stop schedulers to prevent new tasks from being accepted
	log.Info().Msg("Stopping schedulers to prevent new replication tasks")
	s.Stop(ctx)

	// Wait for in-progress tasks with timeout
	log.Info().Msg("Waiting for in-progress replication tasks to complete")
	shutdownDone := make(chan struct{})
	go func() {
		err := wg.Wait()
		if err != nil {
			log.Error().Err(err).Msg("Error waiting for goroutines during shutdown")
		}
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		log.Info().Msg("Graceful shutdown completed successfully")
	case <-shutdownCtx.Done():
		log.Warn().Msg("Shutdown timeout exceeded, forcing exit")
		return fmt.Errorf("graceful shutdown timeout exceeded")
	}

	return nil
}

func resolveLocalRegistryEndpoint(cm *config.ConfigManager) (string, error) {
	if cm.GetOwnRegistry() {
		return utils.FormatRegistryURL(cm.GetLocalRegistryURL()), nil
	}
	var data map[string]any
	if err := json.Unmarshal(cm.GetRawZotConfig(), &data); err != nil {
		return "", fmt.Errorf("unmarshalling zot config: %w", err)
	}
	httpData, ok := data["http"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing 'http' section in zot config")
	}
	addr, _ := httpData["address"].(string)
	port, _ := httpData["port"].(string)
	if addr == "" || port == "" {
		return "", fmt.Errorf("missing 'address' or 'port' in zot http config")
	}
	return addr + ":" + port, nil
}

func handleRegistrySetup(ctx context.Context, log *zerolog.Logger, cm *config.ConfigManager, pathConfig *config.PathConfig) error {
	log.Debug().Msg("Setting up local registry")

	if cm.GetOwnRegistry() {
		log.Info().Msg("Configuring own registry")
		if err := utils.HandleOwnRegistry(cm); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			return err
		}
		log.Info().Msg("Own registry configured successfully")
		return nil
	}

	log.Info().Msg("Launching default registry")

	zm := registry.NewZotManager(log.With().Str("component", "zot manager").Logger(), cm.GetRawZotConfig(), pathConfig.ZotTempConfig)

	if err := zm.HandleRegistrySetup(ctx); err != nil {
		return fmt.Errorf("default registry setup failed: %w", err)
	}

	return nil
}
