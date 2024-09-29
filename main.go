package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/satellite"
	"container-registry.com/harbor-satellite/internal/scheduler"
	"container-registry.com/harbor-satellite/internal/server"
	"container-registry.com/harbor-satellite/internal/state"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"golang.org/x/sync/errgroup"

	"github.com/rs/zerolog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Initialize Config and Logger
	if err := initConfig(); err != nil {
		return err
	}

	ctx, cancel := setupContext()
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)
	ctx = logger.AddLoggerToContext(ctx, config.GetLogLevel())
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
	// Process Input (file or URL)
	stateArtifactFetcher, err := processInput(ctx, log)
	if err != nil || stateArtifactFetcher == nil {
		return fmt.Errorf("error processing input: %w", err)
	}

	satelliteService := satellite.NewSatellite(ctx, stateArtifactFetcher, scheduler.GetSchedulerKey())

	g.Go(func() error {
		return satelliteService.Run(ctx)
	})

	log.Info().Msg("Startup complete 🚀")
	return g.Wait()
}

func initConfig() error {
	if err := config.InitConfig(); err != nil {
		return fmt.Errorf("error initializing config: %w", err)
	}
	return nil
}

func setupContext() (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	return ctx, cancel
}

func setupServerApp(ctx context.Context, log *zerolog.Logger) *server.App {
	router := server.NewDefaultRouter("/api/v1")
	router.Use(server.LoggingMiddleware)

	return server.NewApp(
		router,
		ctx,
		log,
		config.AppConfig,
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

func processInput(ctx context.Context, log *zerolog.Logger) (state.StateFetcher, error) {
	input := config.GetInput()

	if utils.IsValidURL(input) {
		return processURLInput(input, log)
	}

	log.Info().Msg("Input is not a valid URL, checking if it is a file path")
	if err := validateFilePath(input, log); err != nil {
		return nil, err
	}

	return processFileInput(log)
}

func processURLInput(input string, log *zerolog.Logger) (state.StateFetcher, error) {
	log.Info().Msg("Input is a valid URL")
	config.SetRemoteRegistryURL(input)

	stateArtifactFetcher := state.NewURLStateFetcher()

	return stateArtifactFetcher, nil
}

func processFileInput(log *zerolog.Logger) (state.StateFetcher, error) {
	stateArtifactFetcher := state.NewFileStateFetcher()
	stateReader, err := stateArtifactFetcher.FetchStateArtifact()
	if err != nil {
		log.Error().Err(err).Msg("Error fetching state artifact from file")
		return nil, err
	}
	config.SetRemoteRegistryURL(stateReader.GetRegistryURL())
	return stateArtifactFetcher, nil
}

func validateFilePath(path string, log *zerolog.Logger) error {
	if utils.HasInvalidPathChars(path) {
		log.Error().Msg("Path contains invalid characters")
		return fmt.Errorf("invalid file path: %s", path)
	}
	if err := utils.GetAbsFilePath(path); err != nil {
		log.Error().Err(err).Msg("No file found")
		return fmt.Errorf("no file found: %s", path)
	}
	return nil
}
