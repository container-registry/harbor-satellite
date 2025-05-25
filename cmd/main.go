package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/internal/watcher"
	"github.com/container-registry/harbor-satellite/pkg/config"

	_ "github.com/joho/godotenv/autoload"
	"golang.org/x/sync/errgroup"
)

func main() {
	// Global context for the entire application
	globalCtx, globalCancel := utils.SetupContext(context.Background())
	defer globalCancel()

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

	satelliteRestartCount := 0

	// Initialize config manager once
	cm, warnings, err := config.InitConfigManager(token, groundControlURL, config.DefaultConfigPath, config.DefaultPrevConfigPath)
	if err != nil {
		fmt.Printf("Error initiating the config manager: %v", err)
		os.Exit(1)
	}

	// Initialize logger with initial config
	globalCtx, log := logger.InitLogger(globalCtx, cm.GetLogLevel(), warnings)

	// Create channel for config change events
	configChangeEventChan := make(chan struct{}, 1)

	// Start file watcher in a separate goroutine
	go func() {
		if err := watcher.WatchChanges(globalCtx, log.With().Str("component", "file watcher").Logger(), config.DefaultConfigPath, configChangeEventChan); err != nil {
			log.Error().Err(err).Msg("File watcher failed")
		}
	}()

	// Hot-reload main loop
	for {
		if globalCtx.Err() != nil {
			log.Info().Msg("Global context cancelled. Exiting main loop.")
			return
		}

		log.Info().Int("restart_count", satelliteRestartCount).Msg("Starting satellite instance")

		// Create a new context for this satellite run
		runCtx, runCancel := context.WithCancel(globalCtx)
		wg, runCtx := errgroup.WithContext(runCtx)

		runCtx, log := logger.InitLogger(runCtx, cm.GetLogLevel(), warnings)

		wg.Go(func() error {
			return satellite.Run(runCtx, wg, runCancel, cm, log)
		})

		// Check if we should restart or exit
		select {
		case <-globalCtx.Done():
			log.Info().Msg("Shutting down satellite")
			globalCancel()
			_ = wg.Wait()
			return

		case <-configChangeEventChan:
			log.Info().Msg("Config change detected, restarting satellite...")
			runCancel()
			satelliteRestartCount++
			_ = wg.Wait()
			continue

		case <-runCtx.Done():
			log.Info().Msg("Shutting down satellite")
			globalCancel()
			_ = wg.Wait()
			return

		default:
			if err != nil {
				log.Error().Err(err).Msg("Satellite instance failed")
				os.Exit(1)
			}
		}
	}
}
