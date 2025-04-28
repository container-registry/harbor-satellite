package main

import (
	"context"
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/registry"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

func main() {
	err := run()
	if err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()
	wg, ctx := errgroup.WithContext(ctx)

	cm, warnings, err := config.InitConfigManager(config.DefaultConfigPath)
	if err != nil {
		fmt.Printf("Error initiating the config: %v", err)
		return err
	}

	ctx, log := logger.InitLogger(ctx, cm.GetLogLevel(), warnings)

	ctx, scheduler := scheduler.InitBasicScheduler(ctx, log)

	go scheduler.ListenForProcessEvent()

	// Handle registry setup
	if err := handleRegistrySetup(wg, log, cancel, cm); err != nil {
		log.Error().Err(err).Msg("Error setting up local registry")
		return err
	}

	err = scheduler.Start()
	if err != nil {
		log.Error().Err(err).Msg("Error starting scheduler")
		return err
	}
	defer scheduler.Stop()

	satelliteService := satellite.NewSatellite(scheduler.GetSchedulerKey(), cm)

	// Write the config to disk, in case any values were enforced at runtime
	if err := cm.WriteConfig(); err != nil {
		log.Error().Err(err).Msg("Error writing config to disk")
		return err
	}

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
