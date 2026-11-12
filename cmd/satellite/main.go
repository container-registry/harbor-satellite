package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/satellite/admission"
	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/satellite/hotreload"
	"github.com/container-registry/harbor-satellite/internal/satellite/identity"
	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/satellite/lane"
	proxyhandler "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	proxymw "github.com/container-registry/harbor-satellite/internal/satellite/proxy/middleware"
	proxyimage "github.com/container-registry/harbor-satellite/internal/satellite/proxy/process/image"
	"github.com/container-registry/harbor-satellite/internal/satellite/secure"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	"github.com/container-registry/harbor-satellite/internal/satellite/watcher"
	"github.com/container-registry/harbor-satellite/internal/shared/crypto"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/utils"
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
	DesiredStateFile       string
	RegistryDataDir        string
	NoRegistryFallback     bool
	FallbackOnly           bool
	HarborRegistryURL      string
	DirectDelivery         bool
	ImageDir               string
	ProxyMode              proxyhandler.Mode
	ProxyPort              int
	ProxyModeExplicit      bool
	StoreGCGrace           time.Duration
	ProxyWriteTimeout      time.Duration
	ProxyMaxConnections    int
	ProxyStreaming         bool
	ProxyPrefetchWorkers   int
	TelemetryAddr          string
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
		DesiredStateFile:       envCfg.DesiredStateFile,
		RegistryDataDir:        envCfg.RegistryDataDir,
		NoRegistryFallback:     envCfg.NoRegistryFallback,
		HarborRegistryURL:      envCfg.HarborRegistryURL,
		DirectDelivery:         envCfg.DirectDelivery,
		ImageDir:               envCfg.ImageDir,
		ProxyMode:              envCfg.ProxyMode,
		ProxyPort:              envCfg.ProxyPort,
		StoreGCGrace:           envCfg.StoreGCGrace,
		ProxyWriteTimeout:      envCfg.ProxyWriteTimeout,
		ProxyMaxConnections:    envCfg.ProxyMaxConnections,
		ProxyStreaming:         envCfg.ProxyStreaming,
		ProxyPrefetchWorkers:   envCfg.ProxyPrefetchConcurrency,
		TelemetryAddr:          envCfg.TelemetryAddr,
	}
	_, opts.ProxyModeExplicit = os.LookupEnv("PROXY_MODE")
	shutdownTimeout := envCfg.ShutdownTimeout

	flag.StringVar(&opts.GroundControlURL, "ground-control-url", opts.GroundControlURL, "URL to ground control")
	flag.BoolVar(&opts.JSONLogging, "json-logging", true, "Enable JSON logging")
	flag.StringVar(&opts.Token, "token", "", "Satellite token")
	flag.BoolVar(&opts.UseUnsecure, "use-unsecure", opts.UseUnsecure, "Use insecure (HTTP) connections to registries")
	flag.Var(&opts.Mirrors, "mirrors", "Override CRI registry config. Format: CRI:registry1,registry2")
	flag.BoolVar(&opts.SPIFFEEnabled, "spiffe-enabled", opts.SPIFFEEnabled, "Enable SPIFFE/SPIRE authentication")
	flag.StringVar(&opts.SPIFFEEndpointSocket, "spiffe-endpoint-socket", opts.SPIFFEEndpointSocket, "SPIFFE Workload API endpoint socket")
	flag.StringVar(&opts.SPIFFEExpectedServerID, "spiffe-expected-server-id", opts.SPIFFEExpectedServerID, "Expected SPIFFE ID of Ground Control server")
	flag.BoolVar(&opts.BYORegistry, "byo-registry", opts.BYORegistry, "Use an external registry as the replication store")
	flag.StringVar(&opts.RegistryURL, "registry-url", opts.RegistryURL, "External registry URL")
	flag.StringVar(&opts.RegistryUsername, "registry-username", opts.RegistryUsername, "External registry username")
	flag.StringVar(&opts.RegistryPassword, "registry-password", "", "External registry password")
	flag.StringVar(&opts.ConfigDir, "config-dir", opts.ConfigDir, "Configuration directory path (default: ~/.config/satellite)")
	flag.StringVar(&opts.DesiredStateFile, "desired-state-file", opts.DesiredStateFile, "Bootstrap desired-state JSON used when state.json is empty (default: <config-dir>/desired-state.json)")
	flag.StringVar(&opts.RegistryDataDir, "registry-data-dir", opts.RegistryDataDir, "Registry data directory (overrides default storage path derived from config-dir)")
	flag.StringVar(&shutdownTimeout, "shutdown-timeout", shutdownTimeout, "Graceful shutdown timeout (e.g., '30s'). Defaults to SHUTDOWN_TIMEOUT env var or 30s")
	flag.BoolVar(&opts.NoRegistryFallback, "no-registry-fallback", opts.NoRegistryFallback, "Disable all CRI registry fallback configuration")
	flag.BoolVar(&opts.FallbackOnly, "fallback-only", false, "Apply CRI registry fallback configs and exit without starting satellite")
	flag.StringVar(&opts.HarborRegistryURL, "harbor-registry-url", opts.HarborRegistryURL, "Override Harbor registry URL from Ground Control (e.g., http://10.0.0.1:8080)")
	flag.BoolVar(&opts.DirectDelivery, "direct-delivery", opts.DirectDelivery, "[Experimental] Write image tarballs directly to k3s/RKE2 agent images directory")
	flag.StringVar(&opts.ImageDir, "image-dir", opts.ImageDir, "Override image directory for direct delivery (auto-detected if empty)")
	flag.Var(&opts.ProxyMode, "proxy-mode", "OCI proxy mode: proxy or replica")
	flag.IntVar(&opts.ProxyPort, "proxy-port", opts.ProxyPort, "Local OCI registry proxy port")
	flag.DurationVar(&opts.ProxyWriteTimeout, "proxy-write-timeout", opts.ProxyWriteTimeout, "Per-write deadline for OCI proxy responses; a client that accepts no data for this long is cut off (0 disables)")
	flag.IntVar(&opts.ProxyMaxConnections, "proxy-max-connections", opts.ProxyMaxConnections, "Maximum concurrent OCI proxy connections (0 is unlimited)")
	flag.StringVar(&opts.TelemetryAddr, "telemetry-addr", opts.TelemetryAddr, "Address for /healthz and /metrics, separate from the OCI proxy (empty disables)")
	flag.BoolVar(&opts.ProxyStreaming, "proxy-streaming", opts.ProxyStreaming, "Answer a proxy-mode manifest miss once the manifest is stored and stream blob misses (ADR-0013); off fills the whole graph first")
	flag.IntVar(&opts.ProxyPrefetchWorkers, "proxy-prefetch-concurrency", opts.ProxyPrefetchWorkers, "Concurrent background graph completions for streaming pull-through")
	flag.DurationVar(&opts.StoreGCGrace, "store-gc-grace", opts.StoreGCGrace, "How long unreferenced local OCI content is kept before garbage collection; 0 deletes it at once")

	flag.Parse()
	flag.Visit(func(current *flag.Flag) {
		if current.Name == "proxy-mode" {
			opts.ProxyModeExplicit = true
		}
	})
	if opts.Token == "" {
		opts.Token = envCfg.Token
	}
	if opts.RegistryPassword == "" {
		opts.RegistryPassword = envCfg.RegistryPassword
	}
	if err := validateSatelliteOptions(opts); err != nil {
		fmt.Printf("Invalid arguments: %v\n", err)
		os.Exit(1)
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

	// Override StoreDir if --registry-data-dir flag or env var is set.
	if opts.RegistryDataDir != "" {
		pathConfig.StoreDir = opts.RegistryDataDir
	}
	if opts.DesiredStateFile != "" {
		desiredStateFile := opts.DesiredStateFile
		if !filepath.IsAbs(desiredStateFile) {
			if abs, err := filepath.Abs(desiredStateFile); err == nil {
				desiredStateFile = abs
			}
		}
		pathConfig.DesiredStateFile = desiredStateFile
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

func validateSatelliteOptions(opts SatelliteOptions) error {
	if opts.FallbackOnly {
		if opts.ProxyModeExplicit {
			return errors.New("--proxy-mode cannot be combined with --fallback-only")
		}
		return nil
	}
	if !opts.ProxyMode.Valid() {
		return fmt.Errorf("--proxy-mode must be %q or %q, got %q", proxyhandler.ModeProxy, proxyhandler.ModeReplica, opts.ProxyMode)
	}
	if opts.ProxyPort < 1 || opts.ProxyPort > 65535 {
		return fmt.Errorf("--proxy-port must be between 1 and 65535, got %d", opts.ProxyPort)
	}
	if opts.ProxyWriteTimeout < 0 {
		return fmt.Errorf("--proxy-write-timeout must not be negative, got %s", opts.ProxyWriteTimeout)
	}
	if opts.ProxyMaxConnections < 0 {
		return fmt.Errorf("--proxy-max-connections must not be negative, got %d", opts.ProxyMaxConnections)
	}
	if opts.ProxyStreaming && opts.ProxyPrefetchWorkers < 1 {
		return fmt.Errorf("--proxy-prefetch-concurrency must be at least 1 with --proxy-streaming, got %d", opts.ProxyPrefetchWorkers)
	}
	if opts.StoreGCGrace < 0 {
		return fmt.Errorf("--store-gc-grace must not be negative, got %s", opts.StoreGCGrace)
	}
	return nil
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
	if opts.FallbackOnly && opts.GroundControlURL == "" {
		opts.GroundControlURL = config.DefaultGroundControlURL
	}

	encryptor := secure.NewConfigEncryptor(crypto.NewAESProvider(), identity.NewLinuxDeviceIdentity())
	cm, warnings, err := config.InitConfigManager(opts.Token, opts.GroundControlURL, pathConfig.ConfigFile, pathConfig.PrevConfigFile, opts.JSONLogging, opts.UseUnsecure, config.WithEncryptor(encryptor))
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

	if opts.FallbackOnly {
		runFallbackOnly(cm, opts, resolveCRIAndApply)
		return nil
	}

	var (
		proxyServer   *http.Server
		proxyListener net.Listener
		localStore    store.Store
		remoteStore   store.Store
	)
	telemetryServer, err := telemetry.NewServer(telemetry.ServerOptions{Addr: opts.TelemetryAddr})
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	recorder := telemetryServer.Recorder()
	fillLeases := store.NewFillLeases()
	localStore, remoteStore, indexRecovered, err := proxyStores(cm, pathConfig.StoreDir, store.OCIStoreOptions{
		AtomicIndex:  true,
		RecoverIndex: true,
		FillLeases:   fillLeases,
		GCGrace:      opts.StoreGCGrace,
		Recorder:     recorder,
	})
	if err != nil {
		return fmt.Errorf("initialize proxy stores: %w", err)
	}

	proxyLifecycleCtx, cancelProxy := context.WithCancel(context.Background())
	defer cancelProxy()
	pullOptions, closePrefetch, err := streamingPullOptions(opts, localStore, remoteStore)
	if err != nil {
		return fmt.Errorf("initialize streaming pull-through: %w", err)
	}
	defer closePrefetch()
	pullOptions.Recorder = recorder
	edgeJournal, err := openJournal(cm, pathConfig.JournalDir)
	if err != nil {
		return fmt.Errorf("open edge journal: %w", err)
	}
	defer func() {
		if err := edgeJournal.Close(); err != nil {
			fmt.Printf("warning: close edge journal: %v\n", err)
		}
	}()
	pullOptions.Journal = edgeJournal
	pullHandler, err := proxyimage.NewPullWithOptions(proxyLifecycleCtx, opts.ProxyMode, localStore, remoteStore, pullOptions)
	if err != nil {
		return fmt.Errorf("initialize OCI proxy pull handler: %w", err)
	}
	proxyAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.ProxyPort))
	proxyServer = &http.Server{
		Addr: proxyAddress,
		// The pull handler records pull metrics itself, so the metrics slot
		// stays nil; the listener is loopback, so authentication needs no
		// verifier until peers (G7).
		Handler: proxyhandler.WriteDeadlineHandler(
			proxyhandler.New(pullHandler).WrapAll(proxymw.Stack(
				admission.Middleware(admission.ConfigSource{CM: cm}), proxymw.Authenticate(nil), nil,
			)...).Handler(),
			opts.ProxyWriteTimeout,
		),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       time.Minute,
	}
	proxyListener, err = net.Listen("tcp", proxyAddress)
	if err != nil {
		return fmt.Errorf("listen for OCI proxy on %s: %w", proxyAddress, err)
	}
	proxyListener = proxyhandler.LimitListener(proxyListener, opts.ProxyMaxConnections)
	if checker, ok := localStore.(interface {
		Check(ctx context.Context) error
	}); ok {
		if err := telemetryServer.AddCheck("store", checker.Check); err != nil {
			return err
		}
	}
	if err := telemetryServer.AddCheck("proxy", func(checkCtx context.Context) error {
		conn, dialErr := (&net.Dialer{Timeout: time.Second}).DialContext(checkCtx, "tcp", proxyAddress)
		if dialErr != nil {
			return fmt.Errorf("OCI proxy: %w", dialErr)
		}
		return conn.Close()
	}); err != nil {
		return err
	}
	if err := serveTelemetry(ctx, wg, telemetryServer, opts.TelemetryAddr); err != nil {
		return err
	}
	defer proxyServer.Close()

	wg.Go(func() error {
		err := proxyServer.Serve(proxyListener)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve OCI proxy: %w", err)
	})

	// Resolve local registry endpoint for CRI mirror config
	localRegistryEndpoint := resolveLocalRegistryEndpoint(opts.ProxyMode, opts.ProxyPort)

	// Resolve and apply CRI configs
	criResults := resolveCRIAndApply(cm, opts.Mirrors, opts.NoRegistryFallback, localRegistryEndpoint)
	reportCRIResults(criResults)

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
	if ociStore, ok := localStore.(*store.OCIStore); ok && opts.StoreGCGrace > 0 {
		collectorCtx := ctx
		wg.Go(func() error {
			return ociStore.RunCollector(collectorCtx, opts.StoreGCGrace, store.GCOptions{
				Grace:        opts.StoreGCGrace,
				Roots:        []store.GCRoots{fillLeases},
				IngestMaxAge: time.Hour,
			})
		})
	}
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
		nil, // Will be set after scheduler creation
	)

	eventChan := make(chan struct{})

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

	eventScheduler := events.NewEventScheduler(log)
	s := satellite.NewSatellite(cm, criResults, pathConfig.StateFile, pathConfig.StoreDir, pathConfig.DesiredStateFile, eventScheduler)
	s.SetStores(localStore, remoteStore)
	s.SetJournal(edgeJournal)
	s.SetRecorder(recorder)
	replicationSource, err := store.NewRegistryStoreWithOptions(func() (store.RegistryOptions, error) {
		options, err := store.ResolveSourceRegistry(cm)
		options.BandwidthBytesPerSec = cm.GetReplicationBandwidth()
		return options, err
	})
	if err != nil {
		return fmt.Errorf("initialize replication source: %w", err)
	}
	s.SetReplicationSource(replicationSource)
	if indexRecovered {
		s.RequireFullResync()
	}

	err = s.Run(ctx)
	if err != nil {
		return fmt.Errorf("unable to start satellite: %w", err)
	}

	if proxyServer != nil {
		log.Info().Str("address", proxyServer.Addr).Str("mode", opts.ProxyMode.String()).Msg("OCI registry proxy is listening")
	}

	for _, s := range s.GetSchedulers() {
		if s.Name() == config.ReplicateStateJobName {
			hotReloadManager.SetStateReplicationScheduler(s)
		}
	}

	return gracefulShutdown(ctx, log, s, proxyServer, wg, shutdownTimeout)
}

func gracefulShutdown(
	ctx context.Context,
	log *zerolog.Logger,
	s *satellite.Satellite,
	proxyServer *http.Server,
	wg *errgroup.Group,
	shutdownTimeout string,
) error {
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
	shutdownDone := make(chan error, 1)
	go func() {
		var shutdownGroup errgroup.Group
		shutdownGroup.Go(func() error {
			s.Stop(shutdownCtx)
			return nil
		})
		if proxyServer != nil {
			shutdownGroup.Go(func() error {
				if err := proxyServer.Shutdown(shutdownCtx); err != nil {
					return fmt.Errorf("shut down OCI proxy: %w", err)
				}
				return nil
			})
		}

		shutdownErr := shutdownGroup.Wait()
		runtimeErr := wg.Wait()
		shutdownDone <- errors.Join(shutdownErr, runtimeErr)
	}()

	select {
	case shutdownErr := <-shutdownDone:
		// Persist state to disk before exit (reuses #228 SaveState logic)
		log.Info().Msg("Persisting state to disk before exit")
		if err := s.PersistState(); err != nil {
			log.Warn().Err(err).Msg("Failed to persist state during shutdown")
		} else {
			log.Info().Msg("State persisted successfully")
		}
		if shutdownErr != nil {
			return fmt.Errorf("runtime shutdown: %w", shutdownErr)
		}
		log.Info().Msg("Graceful shutdown completed successfully")
	case <-shutdownCtx.Done():
		log.Warn().Msg("Shutdown timeout exceeded, forcing exit")
		return fmt.Errorf("graceful shutdown timeout exceeded: %w", shutdownCtx.Err())
	}

	return nil
}

// resolveCRIAndApply determines which CRI configs to apply and applies them.
// Priority: config file registry_fallback > --mirrors flag > --no-registry-fallback/env.
func resolveCRIAndApply(cm *config.ConfigManager, mirrors mirrorFlags, noFallback bool, localRegistry string) []runtime.CRIConfigResult {
	fbCfg := cm.GetRegistryFallbackConfig()
	if strings.TrimSpace(localRegistry) == "" && (fbCfg.Enabled || len(mirrors) > 0) {
		fmt.Println("warning: CRI registry configuration requires a reachable registry endpoint")
		return nil
	}

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

func reportCRIResults(results []runtime.CRIConfigResult) {
	for _, result := range results {
		if result.Success {
			fmt.Printf("CRI %s configured (backup: %s)\n", result.CRI, result.BackupPath)
		} else {
			fmt.Printf("warning: %s config error: %s\n", result.CRI, result.Error)
		}
	}
}

func runFallbackOnly(
	cm *config.ConfigManager,
	opts SatelliteOptions,
	apply func(*config.ConfigManager, mirrorFlags, bool, string) []runtime.CRIConfigResult,
) {
	reportCRIResults(apply(cm, opts.Mirrors, opts.NoRegistryFallback, fallbackRegistryEndpoint(cm)))
	fmt.Println("--fallback-only: CRI configs applied, exiting.")
}

func fallbackRegistryEndpoint(cm *config.ConfigManager) string {
	if !cm.GetOwnRegistry() {
		return ""
	}
	return cm.GetLocalRegistryURL()
}

// serveTelemetry listens before the satellite starts, so a taken port fails
// startup instead of silently dropping /healthz.
func serveTelemetry(ctx context.Context, wg *errgroup.Group, server *telemetry.Server, addr string) error {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for telemetry on %s: %w", addr, err)
	}
	wg.Go(func() error {
		if err := server.Serve(ctx, listener); err != nil {
			return fmt.Errorf("serve telemetry: %w", err)
		}
		return nil
	})
	return nil
}

// prefetchQueueSize bounds manifests waiting for graph completion; beyond it
// a prefetch is dropped and the next pull of that manifest asks again.
const prefetchQueueSize = 256

// streamingPullOptions builds the 5.3 pull-through options. Streaming needs
// the OCI layout as the local store; with a BYO registry it stays off.
func streamingPullOptions(opts SatelliteOptions, localStore, remoteStore store.Store) (proxyimage.PullOptions, func(), error) {
	noop := func() {}
	if !opts.ProxyStreaming || !opts.ProxyMode.AllowsUpstreamPull() {
		return proxyimage.PullOptions{}, noop, nil
	}
	if _, ok := localStore.(store.Filler); !ok {
		fmt.Printf("WARNING: --proxy-streaming needs the local OCI layout; %T keeps the whole-graph fill\n", localStore)
		return proxyimage.PullOptions{}, noop, nil
	}
	prefetchLane, err := lane.New("prefetch", lane.Options{Concurrency: opts.ProxyPrefetchWorkers, QueueSize: prefetchQueueSize})
	if err != nil {
		return proxyimage.PullOptions{}, noop, err
	}
	closeLane := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = prefetchLane.Close(closeCtx)
	}
	prefetcher, err := proxyimage.NewLanePrefetcher(prefetchLane, localStore, remoteStore)
	if err != nil {
		closeLane()
		return proxyimage.PullOptions{}, noop, err
	}
	return proxyimage.PullOptions{Streaming: true, Prefetcher: prefetcher}, closeLane, nil
}

func resolveLocalRegistryEndpoint(proxyMode proxyhandler.Mode, proxyPort int) string {
	if !proxyMode.Valid() {
		return ""
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort))
}

// openJournal opens the edge journal (plan item 6.4) with the sizes of
// app_config.journal.
func openJournal(cm *config.ConfigManager, dir string) (*journal.Journal, error) {
	cfg := cm.GetJournalConfig()
	return journal.Open(journal.Options{
		Dir:             dir,
		MaxSegmentBytes: cfg.MaxSegmentBytes,
		MaxTotalBytes:   cfg.MaxTotalBytes,
		SyncInterval:    10 * time.Millisecond,
	})
}

// proxyStores reports whether the OCI layout's index was unreadable and moved
// aside, in which case state replication has to start over.
func proxyStores(cm *config.ConfigManager, storeRoot string, ociOpts store.OCIStoreOptions) (local, remote store.Store, indexRecovered bool, err error) {
	remoteStore, err := store.NewRegistryStoreWithOptions(func() (store.RegistryOptions, error) {
		return store.ResolveSourceRegistry(cm)
	})
	if err != nil {
		return nil, nil, false, fmt.Errorf("initialize Harbor registry store: %w", err)
	}

	if cm.GetOwnRegistry() {
		localStore, err := store.NewRegistryStoreWithOptions(func() (store.RegistryOptions, error) {
			credentials := cm.GetRemoteRegistryCredentials()
			return store.RegistryOptions{
				Endpoint:  utils.FormatRegistryURL(string(credentials.URL)),
				Username:  credentials.Username,
				Password:  credentials.Password,
				PlainHTTP: cm.UseUnsecure() || strings.HasPrefix(strings.ToLower(string(credentials.URL)), "http://"),
				TLS:       cm.GetTLSConfig(),
			}, nil
		})
		if err != nil {
			return nil, nil, false, fmt.Errorf("initialize BYO registry store: %w", err)
		}
		return localStore, remoteStore, false, nil
	}

	localStore, report, err := store.OpenOCIStore(storeRoot, ociOpts)
	if err != nil {
		return nil, nil, false, fmt.Errorf("initialize OCI layout store: %w", err)
	}
	if report.IndexRecovered {
		fmt.Printf("WARNING: unreadable OCI index moved to %s; replicating every group again\n", report.QuarantinedPath)
	}
	return localStore, remoteStore, report.IndexRecovered, nil
}
