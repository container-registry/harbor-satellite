package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/hotreload"
	"github.com/container-registry/harbor-satellite/internal/satellite/registry"
	"github.com/container-registry/harbor-satellite/internal/satellite/watcher"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"

	"github.com/joho/godotenv"
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
	NoRegistryFallback     bool
	FallbackOnly           bool
	HarborRegistryURL      string
	DirectDelivery         bool
	ImageDir               string
}

func main() {
	_ = godotenv.Load(".env") //nolint:errcheck // .env file is optional

	if err := env.LoadSatellite(); err != nil {
		log.Fatalf("invalid environment: %v", err)
	}

	envCfg := env.Satellite.ApplyDefaults()
	opts := SatelliteOptions{
		GroundControlURL:       envCfg.GroundControlURL,
		UseUnsecure:            envCfg.UseUnsecure,
		SPIFFEEnabled:          envCfg.SPIFFEEnabled,
		SPIFFEEndpointSocket:   envCfg.SPIFFEEndpointSocket,
		SPIFFEExpectedServerID: envCfg.SPIFFEExpectedServerID,
		BYORegistry:            envCfg.BYORegistry,
		RegistryURL:            envCfg.RegistryURL,
		RegistryUsername:       envCfg.RegistryUsername,
		ConfigDir:              envCfg.ConfigDir,
		RegistryDataDir:        envCfg.RegistryDataDir,
		NoRegistryFallback:     envCfg.NoRegistryFallback,
		HarborRegistryURL:      envCfg.HarborRegistryURL,
		DirectDelivery:         envCfg.DirectDelivery,
		ImageDir:               envCfg.ImageDir,
	}
	shutdownTimeout := envCfg.ShutdownTimeout

	flag.StringVar(&opts.GroundControlURL, "ground-control-url", opts.GroundControlURL, "URL to ground control")
	flag.BoolVar(&opts.JSONLogging, "json-logging", true, "Enable JSON logging")
	flag.StringVar(&opts.Token, "token", "", "Satellite token")
	flag.BoolVar(&opts.UseUnsecure, "use-unsecure", opts.UseUnsecure, "Use insecure (HTTP) connections to registries")
	flag.Var(&opts.Mirrors, "mirrors", "Override CRI registry config. Format: CRI:registry1,registry2")
	flag.BoolVar(&opts.SPIFFEEnabled, "spiffe-enabled", opts.SPIFFEEnabled, "Enable SPIFFE/SPIRE authentication")
	flag.StringVar(&opts.SPIFFEEndpointSocket, "spiffe-endpoint-socket", opts.SPIFFEEndpointSocket, "SPIFFE Workload API endpoint socket")
	flag.StringVar(&opts.SPIFFEExpectedServerID, "spiffe-expected-server-id", opts.SPIFFEExpectedServerID, "Expected SPIFFE ID of Ground Control server")
	flag.BoolVar(&opts.BYORegistry, "byo-registry", opts.BYORegistry, "Use external registry instead of embedded Zot")
	flag.StringVar(&opts.RegistryURL, "registry-url", opts.RegistryURL, "External registry URL")
	flag.StringVar(&opts.RegistryUsername, "registry-username", opts.RegistryUsername, "External registry username")
	flag.StringVar(&opts.RegistryPassword, "registry-password", "", "External registry password")
	flag.StringVar(&opts.ConfigDir, "config-dir", opts.ConfigDir, "Configuration directory path (default: ~/.config/satellite)")
	flag.StringVar(&opts.RegistryDataDir, "registry-data-dir", opts.RegistryDataDir, "Registry data directory (overrides default storage path derived from config-dir)")
	flag.StringVar(&shutdownTimeout, "shutdown-timeout", shutdownTimeout, "Graceful shutdown timeout (e.g., '30s'). Defaults to SHUTDOWN_TIMEOUT env var or 30s")
	flag.BoolVar(&opts.NoRegistryFallback, "no-registry-fallback", opts.NoRegistryFallback, "Disable all CRI registry fallback configuration")
	flag.BoolVar(&opts.FallbackOnly, "fallback-only", false, "Apply CRI registry fallback configs and exit without starting satellite")
	flag.StringVar(&opts.HarborRegistryURL, "harbor-registry-url", opts.HarborRegistryURL, "Override Harbor registry URL from Ground Control (e.g., http://10.0.0.1:8080)")
	flag.BoolVar(&opts.DirectDelivery, "direct-delivery", opts.DirectDelivery, "[Experimental] Write image tarballs directly to k3s/RKE2 agent images directory")
	flag.StringVar(&opts.ImageDir, "image-dir", opts.ImageDir, "Override image directory for direct delivery (auto-detected if empty)")

	flag.Parse()
	if opts.Token == "" {
		opts.Token = envCfg.Token
	}
	if opts.RegistryPassword == "" {
		opts.RegistryPassword = envCfg.RegistryPassword
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

	// For --fallback-only mode, relax token/gc-url requirements
	if !opts.FallbackOnly {
		if !opts.SPIFFEEnabled && (opts.Token == "" || opts.GroundControlURL == "") {
			fmt.Println("Missing required arguments: --token and --ground-control-url or matching env vars (or enable SPIFFE with --spiffe-enabled).")
			os.Exit(1)
		}
		if opts.GroundControlURL == "" {
			fmt.Println("Missing required argument: --ground-control-url or GROUND_CONTROL_URL env var.")
			os.Exit(1)
		}
		if opts.HarborRegistryURL == "" {
			fmt.Println("Missing required argument: --harbor-registry-url or HARBOR_REGISTRY_URL env var.")
			os.Exit(1)
		}
	}
	if opts.GroundControlURL == "" {
		opts.GroundControlURL = config.DefaultGroundControlURL
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

// reconfigureAuditOnReload swaps the audit logger to match next when the audit
// settings changed, and records the config.changed event. When audit is being
// disabled, the event is emitted on the still-active logger before the swap so
// the disable action itself stays audited; otherwise it is emitted after the
// swap so it lands in the (possibly newly enabled or redirected) destination.
// Returns the audit config now in effect.
// auditLoggerConfig maps the on-disk audit config onto the logger's config,
// resolving defaults and selecting the syslog target. The master Enabled flag
// gates the logger; each transport then has its own enable (syslog defaults on,
// otel needs an explicit enabled flag and endpoint), so either can run alone.
func auditLoggerConfig(c config.AuditConfig) logger.AuditConfig {
	s := c.Syslog
	return logger.AuditConfig{
		Enabled: c.Enabled,
		Syslog: logger.SyslogConfig{
			Enabled:    c.Enabled && s.EnabledOrDefault(),
			Target:     logger.SyslogTarget(s.TargetOrDefault()),
			Tag:        s.TagOrDefault(),
			SocketPath: s.SocketPath,
			Network:    s.Network,
			Address:    s.Address,
			File: logger.SyslogFileConfig{
				Path:       s.File.Path,
				MaxSizeMB:  s.File.MaxSizeMBOrDefault(),
				MaxBackups: s.File.MaxBackupsOrDefault(),
				MaxAgeDays: s.File.MaxAgeDaysOrDefault(),
				Compress:   s.File.CompressOrDefault(),
			},
		},
		OTel: logger.OTelConfig{
			Enabled:  c.Otel.Enabled,
			Endpoint: c.Otel.Endpoint,
		},
	}
}

func reconfigureAuditOnReload(audit *logger.AuditLogger, current, next config.AuditConfig, changedKeys []string, log *zerolog.Logger) config.AuditConfig {
	logChanged := func(outcome logger.Outcome, reason logger.Reason) {
		audit.Log(logger.AuditEvent{
			Operation:    logger.OpUpdate,
			ResourceType: logger.ResConfig,
			Outcome:      outcome,
			Actor:        "satellite",
			ActorType:    logger.ActorSystem,
			Reason:       reason,
			Details: map[string]any{
				"changed_keys": changedKeys,
				"source":       "hot_reload",
			},
		})
	}

	changed := !next.Equal(current)
	disabling := changed && current.Enabled && !next.Enabled
	if disabling {
		// The disable action itself succeeds; it is emitted before the swap on
		// the still-active logger.
		logChanged(logger.OutcomeSuccess, "")
	}
	// Default to success; downgrade to failure if the reconfigure attempt fails
	// so the emitted event does not claim a change that did not take effect.
	outcome, reason := logger.OutcomeSuccess, logger.Reason("")
	if changed {
		if rcErr := audit.Reconfigure(auditLoggerConfig(next)); rcErr != nil {
			// The reconfigure failed, so the audit logger keeps its previous
			// configuration. If audit was already enabled (a writable
			// destination) the failure event below still lands there; if it was
			// disabled there is no writable audit sink yet, so this operator log
			// is the only durable record of the failed enable attempt.
			log.Error().Err(rcErr).Bool("audit_enabled", audit.Enabled()).
				Msg("Failed to reconfigure audit logger after reload; previous audit configuration retained")
			outcome, reason = logger.OutcomeFailure, logger.ReasonReconfigureFailed
		} else {
			current = next
			log.Info().Bool("enabled", audit.Enabled()).Msg("Audit logger reconfigured after hot reload")
		}
	}
	if !disabling {
		logChanged(outcome, reason)
	}
	return current
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

	if opts.HarborRegistryURL != "" {
		cm.With(config.SetHarborRegistryURL(opts.HarborRegistryURL))

		// Apply override to existing state config (from prior ZTR)
		if cm.IsZTRDone() {
			sc := cm.GetStateConfig()
			sc, err = config.ApplyHarborRegistryOverride(sc, opts.HarborRegistryURL)
			if err != nil {
				return fmt.Errorf("apply harbor registry URL override: %w", err)
			}
			cm.With(config.SetStateConfig(sc))
		}
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

	// Resolve and apply CRI configs
	criResults := resolveCRIAndApply(cm, opts.Mirrors, opts.NoRegistryFallback, localRegistryEndpoint)
	for _, r := range criResults {
		if r.Success {
			fmt.Printf("CRI %s configured (backup: %s)\n", r.CRI, r.BackupPath)
		} else {
			fmt.Printf("warning: %s config error: %s\n", r.CRI, r.Error)
		}
	}

	if opts.FallbackOnly {
		fmt.Println("--fallback-only: CRI configs applied, exiting.")
		return nil
	}

	// Configure direct delivery if enabled (after fallback-only exit). This
	// feature shipped in c2dbea8 (#356).
	if opts.DirectDelivery {
		imageDir := opts.ImageDir
		if imageDir == "" {
			imageDir = runtime.DetectImageDir()
		}
		if imageDir == "" {
			return fmt.Errorf("--direct-delivery enabled but no k3s/RKE2 image directory found; use --image-dir to specify one")
		}
		if err := os.MkdirAll(imageDir, 0o755); err != nil {
			return fmt.Errorf("create image directory %s: %w", imageDir, err)
		}
		cm.With(config.SetDirectDelivery(config.DirectDeliveryConfig{
			Enabled:  true,
			ImageDir: imageDir,
		}))
		fmt.Printf("EXPERIMENTAL: direct delivery enabled, images will be written to %s\n", imageDir)
	}

	ctx, log := logger.InitLogger(ctx, cm.GetLogLevel(), opts.JSONLogging, warnings)

	// Initialize audit logger from config and attach to context
	auditCfg := cm.GetAuditConfig()
	audit, auditErr := logger.NewAuditLogger(auditLoggerConfig(auditCfg), logger.ComponentSatellite)
	if auditErr != nil {
		return fmt.Errorf("failed to initialize audit logger: %w", auditErr)
	}
	// currentAuditCfg tracks the live audit settings so a hot reload can detect
	// audit-specific changes and rebuild the logger in place.
	currentAuditCfg := auditCfg
	ctx = logger.WithAuditLogger(ctx, audit)
	if audit.Enabled() {
		log.Info().
			Str("target", auditCfg.Syslog.TargetOrDefault()).
			Msg("Audit logging enabled")
	}

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

	// Process config file change events
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
						changedKeys := make([]string, 0, len(changes))
						for _, c := range changes {
							changedKeys = append(changedKeys, string(c.Type))
						}
						// Swap the audit logger if its settings changed and record
						// the config.changed event. When audit is being disabled the
						// event is emitted before the swap so the disable action is
						// still captured.
						currentAuditCfg = reconfigureAuditOnReload(audit, currentAuditCfg, cm.GetAuditConfig(), changedKeys, log)
						if err := hotReloadManager.ProcessConfigChanges(changes); err != nil {
							log.Error().Err(err).Msg("Error processing configuration changes")
						}
					}
				}
			}
		}
	})

	s := satellite.NewSatellite(cm, criResults, pathConfig.StateFile)
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
	shutdownCtx, shutdownCancel := context.WithTimeout(context.WithValue(context.Background(), logger.LoggerKey, log), shutdownDuration)
	defer shutdownCancel()

	// Stop schedulers to prevent new tasks from being accepted
	log.Info().Msg("Stopping schedulers to prevent new replication tasks")

	// Wait for in-progress tasks and scheduler goroutines with timeout
	log.Info().Msg("Waiting for in-progress replication tasks and scheduler goroutines to complete")
	shutdownDone := make(chan struct{})
	go func() {
		// Stop schedulers (blocks until scheduler goroutines complete)
		s.Stop(shutdownCtx)
		// Wait for errgroup tasks
		err := wg.Wait()
		if err != nil {
			log.Error().Err(err).Msg("Error waiting for goroutines during shutdown")
		}
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		// Persist state to disk before exit (reuses #228 SaveState logic)
		log.Info().Msg("Persisting state to disk before exit")
		if err := s.PersistState(); err != nil {
			log.Warn().Err(err).Msg("Failed to persist state during shutdown")
		} else {
			log.Info().Msg("State persisted successfully")
		}
		log.Info().Msg("Graceful shutdown completed successfully")
	case <-shutdownCtx.Done():
		log.Warn().Msg("Shutdown timeout exceeded, forcing exit")
		return fmt.Errorf("graceful shutdown timeout exceeded")
	}

	return nil
}

// resolveCRIAndApply determines which CRI configs to apply and applies them.
// Priority: config file registry_fallback > --mirrors flag > --no-registry-fallback/env.
func resolveCRIAndApply(cm *config.ConfigManager, mirrors mirrorFlags, noFallback bool, localRegistry string) []runtime.CRIConfigResult {
	fbCfg := cm.GetRegistryFallbackConfig()

	// Config file registry_fallback takes highest priority (from GC)
	if fbCfg.Enabled {
		configs, err := runtime.ResolveCRIConfigs(nil, true, fbCfg.Registries, fbCfg.Runtimes)
		if err != nil {
			fmt.Printf("warning: failed to resolve CRI configs: %v\n", err)
			return nil
		}
		return runtime.ApplyCRIConfigs(configs, localRegistry)
	}

	// Explicit --mirrors flag
	if len(mirrors) > 0 {
		configs, err := runtime.ResolveCRIConfigs(mirrors, false, nil, nil)
		if err != nil {
			fmt.Printf("warning: failed to parse mirror flags: %v\n", err)
			return nil
		}
		return runtime.ApplyCRIConfigs(configs, localRegistry)
	}

	// Disabled via flag or env var
	if noFallback {
		return nil
	}

	// No CRI config requested
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
	addr, addrOK := httpData["address"].(string)
	port, portOK := httpData["port"].(string)
	if !addrOK || !portOK || addr == "" || port == "" {
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
