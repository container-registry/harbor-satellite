package cmd

import (
	"context"
	"fmt"

	runtime "container-registry.com/harbor-satellite/cmd/container_runtime"
	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/satellite"
	"container-registry.com/harbor-satellite/internal/scheduler"
	"container-registry.com/harbor-satellite/internal/server"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"container-registry.com/harbor-satellite/registry"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "harbor-satellite",
		Short: "Harbor Satellite is a tool to replicate images from source registry to Harbor registry",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return utils.CommandRunSetup(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := utils.SetupContext(cmd.Context())
			return run(ctx)
		},
	}
	rootCmd.AddCommand(runtime.NewContainerdCommand())
	rootCmd.AddCommand(runtime.NewCrioCommand())
	return rootCmd
}

func Execute() error {
	return NewRootCommand().Execute()
}

func run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	log := logger.FromContext(ctx)

	// Set up router and app
	app := setupServerApp(ctx, log)
	app.SetupRoutes()
	app.SetupServer(g)

	// Handle registry setup
	if err := handleRegistrySetup(g, log); err != nil {
		return err
	}
	scheduler := scheduler.NewBasicScheduler(ctx)
	ctx = context.WithValue(ctx, scheduler.GetSchedulerKey(), scheduler)
	err := scheduler.Start()
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

func handleRegistrySetup(g *errgroup.Group, log *zerolog.Logger) error {
	if config.GetOwnRegistry() {
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
