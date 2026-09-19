package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite"
	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/satellite/hotreload"
	proxyhandler "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	proxyimage "github.com/container-registry/harbor-satellite/internal/satellite/proxy/process/image"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/satellite/watcher"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

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

func run(parent context.Context, opts *SatelliteOptions, pathConfig *config.PathConfig, cm *config.ConfigManager, warnings []string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	wg, ctx := errgroup.WithContext(ctx)

	var (
		proxyServer   *http.Server
		proxyListener net.Listener
		localStore    store.Store
		remoteStore   store.Store
	)
	localStore, remoteStore, err := proxyStores(cm, pathConfig.StoreDir)
	if err != nil {
		return fmt.Errorf("initialize proxy stores: %w", err)
	}

	proxyLifecycleCtx, cancelProxy := context.WithCancel(context.Background())
	defer cancelProxy()
	proxyAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.ProxyPort))
	proxyServer = &http.Server{
		Addr:              proxyAddress,
		Handler:           proxyhandler.New(proxyimage.NewPull(proxyLifecycleCtx, opts.ProxyMode, localStore, remoteStore)).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       time.Minute,
	}
	proxyListener, err = net.Listen("tcp", proxyAddress)
	if err != nil {
		return fmt.Errorf("listen for OCI proxy on %s: %w", proxyAddress, err)
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
	for _, r := range criResults {
		if r.Success {
			fmt.Printf("CRI %s configured (backup: %s)\n", r.CRI, r.BackupPath)
		} else {
			fmt.Printf("warning: %s config error: %s\n", r.CRI, r.Error)
		}
	}

	// Configure direct delivery if enabled. This feature shipped in c2dbea8 (#356).
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
	s := satellite.NewSatellite(cm, criResults, pathConfig.StateFile, pathConfig.StoreDir, eventScheduler)
	s.SetStores(localStore, remoteStore)

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

	return gracefulShutdown(ctx, log, s, proxyServer, wg, opts.ShutdownTimeout)
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
		return fmt.Errorf("graceful shutdown timeout exceeded")
	}

	return nil
}

// resolveCRIAndApply determines which CRI configs to apply and applies them.
// Priority: config file registry_fallback > --mirrors flag > --no-registry-fallback/env.
func resolveCRIAndApply(cm *config.ConfigManager, mirrors mirrorFlags, noFallback bool, localRegistry string) []runtime.CRIConfigResult {
	fbCfg := cm.GetRegistryFallbackConfig()
	if strings.TrimSpace(localRegistry) == "" && (fbCfg.Enabled || len(mirrors) > 0) {
		fmt.Println("warning: CRI registry configuration requires --proxy-mode")
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

func resolveLocalRegistryEndpoint(proxyMode proxyhandler.Mode, proxyPort int) string {
	if !proxyMode.Valid() {
		return ""
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort))
}

func proxyStores(cm *config.ConfigManager, storeRoot string) (store.Store, store.Store, error) {
	remoteStore, err := store.NewDynamicRegistryStore(func() (store.RegistryOptions, error) {
		return sourceRegistryOptions(cm)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("initialize Harbor registry store: %w", err)
	}

	if cm.GetOwnRegistry() {
		credentials := cm.GetRemoteRegistryCredentials()
		localStore, err := store.NewRegistryStore(store.RegistryOptions{
			Endpoint:  utils.FormatRegistryURL(string(credentials.URL)),
			Username:  credentials.Username,
			Password:  credentials.Password,
			PlainHTTP: store.UsesPlainHTTP(string(credentials.URL), cm.UseUnsecure()),
			TLS:       cm.GetTLSConfig(),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("initialize BYO registry store: %w", err)
		}
		return localStore, remoteStore, nil
	}

	localStore, err := store.NewOCIStore(storeRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize OCI layout store: %w", err)
	}
	return localStore, remoteStore, nil
}

func sourceRegistryOptions(cm *config.ConfigManager) (store.RegistryOptions, error) {
	credentials := cm.GetSourceRegistryCredentials()
	sourceURL := string(credentials.URL)
	if override := cm.GetHarborRegistryURL(); override != "" {
		if sourceURL == "" {
			sourceURL = override
		} else {
			replaced, err := config.ReplaceURLHost(sourceURL, override)
			if err != nil {
				return store.RegistryOptions{}, fmt.Errorf("apply Harbor registry override: %w", err)
			}
			sourceURL = replaced
		}
	}
	return store.RegistryOptions{
		Endpoint:  utils.FormatRegistryURL(sourceURL),
		Username:  credentials.Username,
		Password:  credentials.Password,
		PlainHTTP: store.UsesPlainHTTP(sourceURL, cm.UseUnsecure()),
		TLS:       cm.GetTLSConfig(),
	}, nil
}
