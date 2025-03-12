package cmd

import (
	"context"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/server"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/registry"
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
		var defaultZotConfig registry.DefaultZotConfig
		err := registry.ReadConfig(config.GetZotConfigPath(), &defaultZotConfig)
		if err != nil {
			return fmt.Errorf("error reading config: %w", err)
		}
		defaultZotURL := defaultZotConfig.GetLocalRegistryURL()
		config.SetRemoteRegistryURL(defaultZotURL)
		g.Go(func() error {
			log.Info().Msg("Launching default registry")
			if err := utils.LaunchDefaultZotRegistry(); err != nil {
				log.Error().Err(err).Msg("Error launching default registry")
				cancel()
				return err
			}
			cancel()
			return nil
		})
	}
	return nil
}
