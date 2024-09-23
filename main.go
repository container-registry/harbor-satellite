package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/images"
	"container-registry.com/harbor-satellite/internal/replicate"
	"container-registry.com/harbor-satellite/internal/satellite"
	"container-registry.com/harbor-satellite/internal/server"
	"container-registry.com/harbor-satellite/internal/store"
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
	log.Info().Msg("Satellite starting")

	// Set up router and app
	app := setupServerApp(ctx, log)
	app.SetupRoutes()
	app.SetupServer(g)

	// Handle registry setup
	if err := handleRegistrySetup(g, log, cancel); err != nil {
		return err
	}

	// Process Input (file or URL)
	fetcher, err := processInput(ctx, log)
	if err != nil {
		return err
	}

	ctx, storer := store.NewInMemoryStore(ctx, fetcher)
	replicator := replicate.NewReplicator(ctx)
	satelliteService := satellite.NewSatellite(ctx, storer, replicator)

	g.Go(func() error {
		return satelliteService.Run(ctx)
	})

	log.Info().Msg("Satellite running")
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

func processInput(ctx context.Context, log *zerolog.Logger) (store.ImageFetcher, error) {
	input := config.GetInput()
	if !utils.IsValidURL(input) {
		log.Info().Msg("Input is not a valid URL, checking if it is a file path")
		if err := validateFilePath(config.GetInput(), log); err != nil {
			return nil, err
		}
		return setupFileFetcher(ctx, log)
	}

	log.Info().Msg("Input is a valid URL")
	config.SetRemoteRegistryURL(input)
	state_arifact_fetcher := config.NewURLStateArtifactFetcher()
	if err := state_arifact_fetcher.FetchStateArtifact(); err != nil {
		log.Error().Err(err).Msg("Error fetching state artifact")
		return nil, err
	}
	fetcher := store.RemoteImageListFetcher(ctx, input)
	// utils.SetUrlConfig(input)
	return fetcher, nil
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

func setupFileFetcher(ctx context.Context, log *zerolog.Logger) (store.ImageFetcher, error) {
	fetcher := store.FileImageListFetcher(ctx, config.GetInput())
	var imagesList images.ImageList
	if err := utils.ParseImagesJsonFile(config.GetInput(), &imagesList); err != nil {
		log.Error().Err(err).Msg("Error parsing images.json file")
		return nil, err
	}
	if err := utils.SetRegistryEnvVars(imagesList); err != nil {
		log.Error().Err(err).Msg("Error setting registry environment variables")
		return nil, err
	}
	return fetcher, nil
}
