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
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()
	wg, ctx := errgroup.WithContext(ctx)
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

	cm, warnings, err := config.InitConfigManager(token, groundControlURL, config.DefaultConfigPath, config.DefaultPrevConfigPath)
	ctx, log := logger.InitLogger(ctx, cm.GetLogLevel(), warnings)
	if err != nil {
		fmt.Printf("Error initiating the config manager: %v", err)
		os.Exit(1)
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

	err = satellite.Run(ctx, wg, cancel, cm, log)
	if err != nil {
		os.Exit(1)
	}
}
