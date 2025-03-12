package cmd

import (
	"context"
	"fmt"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/logger"
	"container-registry.com/harbor-satellite/internal/satellite"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/registry"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

func Run() error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()

	ctx, wg, scheduler, err := utils.Init(ctx)
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)

	go scheduler.ListenForProcessEvent()

	// Handle registry setup
	if err := handleRegistrySetup(wg, log, cancel); err != nil {
		log.Error().Err(err).Msg("Error setting up local registry")
		return err
	}

	err = scheduler.Start()
	if err != nil {
		log.Error().Err(err).Msg("Error starting scheduler")
		return err
	}

	localRegistryConfig := satellite.NewRegistryConfig(config.GetRemoteRegistryURL(), config.GetRemoteRegistryUsername(), config.GetRemoteRegistryPassword())
	sourceRegistryConfig := satellite.NewRegistryConfig(config.GetSourceRegistryURL(), config.GetSourceRegistryUsername(), config.GetSourceRegistryPassword())
	satelliteService := satellite.NewSatellite(ctx, scheduler.GetSchedulerKey(), localRegistryConfig, sourceRegistryConfig, config.UseUnsecure(), config.GetStates())

	wg.Go(func() error {
		return satelliteService.Run(ctx)
	})

	wg.Wait()
	scheduler.Stop()
	return nil
}

func handleRegistrySetup(g *errgroup.Group, log *zerolog.Logger, cancel context.CancelFunc) error {
	log.Debug().Msg("Setting up local registry")
	if config.GetOwnRegistry() {
		log.Info().Msg("Configuring own registry")
		if err := utils.HandleOwnRegistry(); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			return err
		}
	} else {
		var defaultZotConfig registry.DefaultZotConfig
		err := registry.ReadConfig(config.GetZotConfigPath(), &defaultZotConfig)
		if err != nil {
			return fmt.Errorf("error reading config: %w", err)
		}
		defaultZotURL := defaultZotConfig.GetLocalRegistryURL()
		config.SetRemoteRegistryURL(defaultZotURL)
		g.Go(func() error {
			log.Info().Msg("Launching default registry")
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
