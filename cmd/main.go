package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/container-registry/harbor-satellite/internal/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/hotreload"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/registry"
	"github.com/container-registry/harbor-satellite/internal/satellite"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/internal/watcher"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"os"

	_ "github.com/joho/godotenv/autoload"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

// mirrorFlags is a custom flag type for cri and mirror mappings
type mirrorFlags []string

// String implements flag.Value for mirrorFlags
func (m *mirrorFlags) String() string {
	return fmt.Sprint(*m)
}

func (m *mirrorFlags) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func main() {
	var jsonLogging bool
	var groundControlURL string
	var token string
	var mirrors mirrorFlags

	flag.StringVar(&groundControlURL, "ground-control-url", "", "URL to ground control")
	flag.BoolVar(&jsonLogging, "json-logging", true, "Enable JSON logging")
	flag.StringVar(&token, "token", "", "Satellite token")
	flag.Var(&mirrors, "mirrors", "Specify CRI and registries in the form CRI:registry1,registry2")

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

	cm, _, err := config.InitConfigManager(token, groundControlURL, config.DefaultConfigPath, config.DefaultPrevConfigPath, jsonLogging)
	if err != nil {
		fmt.Printf("Error initiating the config manager: %v", err)
		os.Exit(1)
	}

	// get local registry addrress from raw zot config
	var data map[string]interface{}
	if err := json.Unmarshal(cm.GetRawZotConfig(), &data); err != nil {
		panic(err)
	}
	httpData := data["http"].(map[string]interface{})
	localRegistryEndpoint := httpData["address"].(string) + ":" + httpData["port"].(string)

	err = runtime.ApplyCRIConfigs(mirrors, localRegistryEndpoint)
	if err != nil {
		fmt.Printf("fatal : %v\n", err)
		os.Exit(1)
	}

	err = run(jsonLogging, token, groundControlURL)
	if err != nil {
		fmt.Printf("fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(jsonLogging bool, token, groundControlURL string) error {
	ctx, cancel := utils.SetupContext(context.Background())
	defer cancel()
	wg, ctx := errgroup.WithContext(ctx)

	cm, warnings, err := config.InitConfigManager(token, groundControlURL, config.DefaultConfigPath, config.DefaultPrevConfigPath, jsonLogging)
	if err != nil {
		fmt.Printf("Error initiating the config manager: %v", err)
		return err
	}

	ctx, log := logger.InitLogger(ctx, cm.GetLogLevel(), jsonLogging, warnings)

	// Write the config to disk, in case any defaults were enforced at runtime
	if err := cm.WriteConfig(); err != nil {
		log.Error().Err(err).Msg("Error writing config to disk")
		return err
	}

	hotReloadManager := hotreload.NewHotReloadManager(
		ctx,
		cm,
		log,
		nil, // Will be set after scheduler creation
	)

	eventChan := make(chan struct{})

	// Handle registry setup
	wg.Go(func() error { return handleRegistrySetup(ctx, log, cm) })

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
				changes, warnings, err := cm.ReloadConfig()
				if err != nil {
					log.Error().Err(err).Msg("Failed to reload configuration")
				} else {
					if len(warnings) > 0 {
						for _, warning := range warnings {
							log.Warn().Str("warning", warning).Msg("Configuration reload warning")
						}
					}
					if len(changes) > 0 {
						if err := hotReloadManager.ProcessConfigChanges(changes); err != nil {
							log.Error().Err(err).Msg("Error processing configuration changes")
						}
					}
				}
			}
		}
	})

	s := satellite.NewSatellite(cm)
	err = s.Run(ctx)
	if err != nil {
		return fmt.Errorf("unable to start satellite: %w", err)
	}

	for _, s := range s.GetSchedulers() {
		if s.Name() == config.ReplicateStateJobName {
			hotReloadManager.SetStateReplicationScheduler(s)
		}
	}

	// Wait until context is cancelled
	<-ctx.Done()
	log.Info().Msg("Satellite context cancelled, shutting down...")

	return wg.Wait()
}

func handleRegistrySetup(ctx context.Context, log *zerolog.Logger, cm *config.ConfigManager) error {
	log.Debug().Msg("Setting up local registry")

	if cm.GetOwnRegistry() {
		log.Info().Msg("Configuring own registry")
		if err := utils.HandleOwnRegistry(cm); err != nil {
			log.Error().Err(err).Msg("Error handling own registry")
			return err
		}
		log.Info().Msg("Own registry configured successfully")
		return nil
	}

	log.Info().Msg("Launching default registry")

	zm := registry.NewZotManager(log.With().Str("component", "zot manager").Logger(), cm.GetRawZotConfig())

	if err := zm.HandleRegistrySetup(ctx); err != nil {
		return fmt.Errorf("default registry setup failed: %w", err)
	}

	return nil
}
