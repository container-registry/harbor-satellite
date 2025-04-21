package main

import (
	"context"
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/registry"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()
	wg, ctx := errgroup.WithContext(ctx)

	cm, warnings, err := utils.InitConfig(config.DefaultConfigPath)
	if err != nil {
		fmt.Printf("Error initiating the config: %v", err)
		return err
	}

	ctx, log := utils.InitLogger(ctx, cm.GetLogLevel(), warnings)

	ctx, scheduler := utils.InitScheduler(ctx)

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
		var defaultZotConfig registry.ZotConfig
		if err := registry.ReadZotConfig(cm.GetZotConfigPath(), &defaultZotConfig); err != nil {
			log.Error().Err(err).Msg("Error launching default zot registry")
			return fmt.Errorf("error reading config: %w", err)
		}

		// TODO: Is this code block necessary?
		err := cm.With(config.SetLocalRegistryURL(defaultZotConfig.GetRegistryURL()))
		if err != nil {
			return fmt.Errorf("error setting RemoteRegistryURL")
		}

		g.Go(func() error {
			if err := registry.LaunchRegistry(cm.GetZotConfigPath()); err != nil {
				log.Error().Err(err).Msg("Error launching default zot registry")
				cancel()
				return fmt.Errorf("error launching default zot registry: %w", err)
			}
			cancel()
			return nil
		})
	}
	return nil
}
