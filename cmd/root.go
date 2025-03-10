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
			ctx, cancel := utils.SetupContext(cmd.Context())
			return run(ctx, cancel)
		},
	}
	rootCmd.AddCommand(runtime.NewContainerdCommand())
	rootCmd.AddCommand(runtime.NewCrioCommand())
	return rootCmd
}

func Execute() error {
	return NewRootCommand().Execute()
}

func run(ctx context.Context, cancel context.CancelFunc) error {
	g, ctx := errgroup.WithContext(ctx)
	log := logger.FromContext(ctx)

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
	err := scheduler.Start()
	if err != nil {
		log.Error().Err(err).Msg("Error starting scheduler")
		return err
	}

	localRegistryConfig := satellite.NewRegistryConfig(config.GetRemoteRegistryURL(),
		config.GetRemoteRegistryUsername(), config.GetRemoteRegistryPassword())
	sourceRegistryConfig := satellite.NewRegistryConfig(config.GetSourceRegistryURL(),
		config.GetSourceRegistryUsername(), config.GetSourceRegistryPassword())

	satelliteService := satellite.NewSatellite(ctx, scheduler.GetSchedulerKey(), localRegistryConfig,
		sourceRegistryConfig, config.UseUnsecure(), config.GetStates())

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
	defer fmt.Println("calling cancel func")
	if config.GetOwnRegistry() {
		if err := utils.HandleOwnRegistry(); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			cancel()
			return err
		}
	} else {
		defer cancel()
		log.Info().Msg("Launching default registry")
		var defaultZotConfig registry.ZotConfig
		if err := registry.ReadZotConfig(config.GetZotConfigPath(), &defaultZotConfig); err != nil {
			log.Error().Err(err).Msg("Error launching default zot registry")
			return fmt.Errorf("error reading config: %w", err)
		}

		if err := defaultZotConfig.Validate(); err != nil {
			log.Error().Err(err).Msg("Error launching default zot registry")
			return fmt.Errorf("invalid zot config: %w", err)
		}

		config.SetRemoteRegistryURL(defaultZotConfig.GetRegistryURL())

		g.Go(func() error {
			if err := registry.LaunchRegistry(config.GetZotConfigPath()); err != nil {
				log.Error().Err(err).Msg("Error launching default zot registry")
				return fmt.Errorf("error launching default zot registry: %w", err)
			}
			return nil
		})
	}
	return nil
}
