package main

import (
	"context"
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/state"
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
		return err
	}

	ctx, log := utils.InitLogger(ctx, warnings)

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

	localRegistryConfig := state.NewRegistryConfig(config.GetRemoteRegistryURL(), config.GetRemoteRegistryUsername(), config.GetRemoteRegistryPassword())
	sourceRegistryConfig := state.NewRegistryConfig(config.GetSourceRegistryURL(), config.GetSourceRegistryUsername(), config.GetSourceRegistryPassword())
	satelliteService := satellite.NewSatellite(ctx, scheduler.GetSchedulerKey(), localRegistryConfig, sourceRegistryConfig, config.UseUnsecure(), config.GetState())

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
	if config.GetOwnRegistry() {
		log.Info().Msg("Configuring own registry")
		if err := utils.HandleOwnRegistry(); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			cancel()
			return err
		}
	} else {
		log.Info().Msg("Launching default registry")
		var defaultZotConfig registry.ZotConfig
		if err := registry.ReadZotConfig(config.GetZotConfigPath(), &defaultZotConfig); err != nil {
			log.Error().Err(err).Msg("Error launching default zot registry")
			return fmt.Errorf("error reading config: %w", err)
		}

		// TODO: Is this code block necessary?
		err := cm.With(config.SetLocalRegistryCredentials(config.RegistryCredentials{URL: config.URL(defaultZotConfig.GetRegistryURL())}))
		if err != nil {
			return fmt.Errorf("error setting RemoteRegistryURL")
		}

		g.Go(func() error {
			if err := registry.LaunchRegistry(config.GetZotConfigPath()); err != nil {
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
