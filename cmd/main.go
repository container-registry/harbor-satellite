package cmd

import (
	"context"
	"fmt"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/logger"
	"container-registry.com/harbor-satellite/internal/satellite"
	"container-registry.com/harbor-satellite/internal/server"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/registry"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

func Run() error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()

	ctx, wg, scheduler, err := utils.Init(ctx)
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)

	go scheduler.ListenForProcessEvent()

	// Set up router and app
	log.Debug().Msg("Setting up http server")
	app := setupServerApp(ctx, log)
	app.SetupRoutes()
	app.SetupServer(wg)

	// Handle registry setup
	if err := handleRegistrySetup(wg, log, cancel); err != nil {
		log.Error().Err(err).Msg("Error setting up local registry")
		return err
	}

	err = scheduler.Start()
	if err != nil {
		log.Error().Err(err).Msg("Error starting scheduler")
		return err
	}

	localRegistryConfig := satellite.NewRegistryConfig(config.GetRemoteRegistryURL(), config.GetRemoteRegistryUsername(), config.GetRemoteRegistryPassword())
	sourceRegistryConfig := satellite.NewRegistryConfig(config.GetSourceRegistryURL(), config.GetSourceRegistryUsername(), config.GetSourceRegistryPassword())
	satelliteService := satellite.NewSatellite(ctx, scheduler.GetSchedulerKey(), localRegistryConfig, sourceRegistryConfig, config.UseUnsecure(), config.GetStates())

	wg.Go(func() error {
		return satelliteService.Run(ctx)
	})

	wg.Wait()
	scheduler.Stop()
	return nil
}

func setupServerApp(ctx context.Context, log *zerolog.Logger) *server.App {
	router := server.NewDefaultRouter("/api/v1")
	router.Use(server.LoggingMiddleware)

	return server.NewApp(
		router,
		ctx,
		log,
		&server.MetricsRegistrar{},
		&server.DebugRegistrar{},
		&satellite.SatelliteRegistrar{},
	)
}

func handleRegistrySetup(g *errgroup.Group, log *zerolog.Logger, cancel context.CancelFunc) error {
	log.Debug().Msg("Setting up local registry")
	if config.GetOwnRegistry() {
		log.Info().Msg("Configuring own registry")
		if err := utils.HandleOwnRegistry(); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			return err
		}
	} else {
		log.Info().Msg("Launching default registry")
		var defaultZotConfig registry.ZotConfig
		err := registry.ReadZotConfig(config.GetZotConfigPath(), &defaultZotConfig)
		if err != nil {
			log.Error().Err(err).Msg("error launching default zot registry")
			return fmt.Errorf("error reading config: %w", err)
		}
		// The names "Local" and "Remote" are confusing here.
		config.SetRemoteRegistryURL(defaultZotConfig.GetLocalRegistryURL())
		g.Go(func() error {
			if err := registry.LaunchRegistry(config.GetZotConfigPath()); err != nil {
				log.Error().Err(err).Msg("error launching default zot registry")
				return fmt.Errorf("error launching default zot registry: %w", err)
			}
			return nil
		})
	}
	return nil
}
