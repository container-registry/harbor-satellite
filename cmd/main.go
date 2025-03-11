package cmd

import (
	"context"
	"fmt"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/satellite"
	"container-registry.com/harbor-satellite/internal/scheduler"
	"container-registry.com/harbor-satellite/internal/server"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/registry"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

func Run() error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()

	log, err := utils.Init(ctx)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	// Set up router and app
	app := setupServerApp(ctx, log)
	app.SetupRoutes()
	app.SetupServer(g)

	// Handle registry setup
	if err := handleRegistrySetup(g, log, cancel); err != nil {
		return err
	}
	scheduler := scheduler.NewBasicScheduler(ctx)
	ctx = context.WithValue(ctx, scheduler.GetSchedulerKey(), scheduler)
	err = scheduler.Start()
	if err != nil {
		log.Error().Err(err).Msg("Error starting scheduler")
		return err
	}
	localRegistryConfig := satellite.NewRegistryConfig(config.GetRemoteRegistryURL(), config.GetRemoteRegistryUsername(), config.GetRemoteRegistryPassword())
	sourceRegistryConfig := satellite.NewRegistryConfig(config.GetSourceRegistryURL(), config.GetSourceRegistryUsername(), config.GetSourceRegistryPassword())
	states := config.GetStates()
	useUnsecure := config.UseUnsecure()
	satelliteService := satellite.NewSatellite(ctx, scheduler.GetSchedulerKey(), localRegistryConfig, sourceRegistryConfig, useUnsecure, states)

	g.Go(func() error {
		return satelliteService.Run(ctx)
	})

	log.Info().Msg("Startup complete 🚀")
	g.Wait()
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
	if config.GetOwnRegistry() {
		if err := utils.HandleOwnRegistry(); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			return err
		}
	} else {
		log.Info().Msg("Launching default registry")
		var defaultZotConfig registry.DefaultZotConfig
		err := registry.ReadConfig(config.GetZotConfigPath(), &defaultZotConfig)
		if err != nil {
			return fmt.Errorf("error reading config: %w", err)
		}
		defaultZotURL := defaultZotConfig.GetLocalRegistryURL()
		config.SetRemoteRegistryURL(defaultZotURL)
		g.Go(func() error {
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
