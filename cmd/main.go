package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/registry"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/internal/watcher"
	"github.com/container-registry/harbor-satellite/pkg/config"

	_ "github.com/joho/godotenv/autoload"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

func main() {
	var groundControlURL string
	var token string

	flag.StringVar(&groundControlURL, "ground-control-url", "", "URL to ground control")
	flag.StringVar(&token, "token", "", "Satellite token")
	flag.Parse()

	if token == "" {
		token = os.Getenv("TOKEN")
	}
	if groundControlURL == "" {
		groundControlURL = os.Getenv("GROUND_CONTROL_URL")
	}

	if token == "" || groundControlURL == "" {
		fmt.Println("Missing required arguments: --token and --ground-control-url or matching env vars.")
		os.Exit(1)
	}

	err := run(token, groundControlURL)
	if err != nil {
		os.Exit(1)
	}
}

func run(token, groundControlURL string) error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()
	wg, ctx := errgroup.WithContext(ctx)

	cm, warnings, err := config.InitConfigManager(token, groundControlURL, config.DefaultConfigPath, config.DefaultPrevConfigPath)
	if err != nil {
		fmt.Printf("Error initiating the config manager: %v", err)
		return err
	}

	ctx, log := logger.InitLogger(ctx, cm.GetLogLevel(), warnings)

	// Handle registry setup
	if err := handleRegistrySetup(wg, log, cancel, cm); err != nil {
		log.Error().Err(err).Msg("Error setting up local registry")
		return err
	}

	satelliteService := satellite.NewSatellite(cm)

	// Write the config to disk, in case any defaults were enforced at runtime
	if err := cm.WriteConfig(); err != nil {
		log.Error().Err(err).Msg("Error writing config to disk")
		return err
	}

	eventChan := make(chan struct{})

	// Watch for changes in the config file
	wg.Go(func() error {
		return watcher.WatchChanges(ctx, log.With().Str("component", "file watcher").Logger(), config.DefaultConfigPath, eventChan)
	})

	// Watch for changes in the config file
	wg.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-eventChan:
				log.Info().Msg("Event chan event received")
			}
		}
	})

	wg.Go(func() error {
		return satelliteService.Run(ctx)
	})

	return wg.Wait()
}

func handleRegistrySetup(g *errgroup.Group, log *zerolog.Logger, cancel context.CancelFunc, cm *config.ConfigManager) error {
	log.Debug().Msg("Setting up local registry")
	if cm.GetOwnRegistry() {
		log.Info().Msg("Configuring own registry")
		if err := utils.HandleOwnRegistry(cm); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			cancel()
			return err
		}
	} else {
		log.Info().Msg("Launching default registry")

		zm := registry.NewZotManager(log, cm.GetRawZotConfig())

		return zm.HandleRegistrySetup(g, cancel)
	}
	return nil
}
