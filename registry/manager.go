package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"zotregistry.dev/zot/pkg/cli/server"
)

type ZotManager struct {
	zotConfig json.RawMessage
	log       *zerolog.Logger
}

func NewZotManager(log *zerolog.Logger, zotConfig json.RawMessage) *ZotManager {
	return &ZotManager{
		zotConfig: zotConfig,
		log:       log,
	}
}

func (zm *ZotManager) HandleRegistrySetup(g *errgroup.Group, cancel context.CancelFunc) error {
	tmpConfigPath, err := zm.WriteTempZotConfig()
	if err != nil {
		zm.log.Error().Err(err).Msg("Error writing temp zot config to disk")
		return fmt.Errorf("error writing temp zot config to disk: %w", err)
	}

	g.Go(func() error {
		defer func() {
			err := zm.RemoveTempZotConfig(tmpConfigPath)
			if err != nil {
				zm.log.Warn().Err(err).Msg("Failed to remove temp zot config")
			} else {
				zm.log.Debug().Str("path", tmpConfigPath).Msg("Temp zot config removed")
			}
		}()

		if err := zm.VerifyRegistryConfig(tmpConfigPath); err != nil {
			zm.log.Error().Err(err).Msg("Error launching default zot registry")
			cancel()
			return fmt.Errorf("error launching default zot registry: %w", err)
		}

		if err := zm.LaunchZotRegistry(tmpConfigPath); err != nil {
			zm.log.Error().Err(err).Msg("Error launching default zot registry")
			cancel()
			return fmt.Errorf("error launching default zot registry: %w", err)
		}
		cancel()

		return nil
	})

	return nil
}

// WriteTempZotConfig creates a temp file and writes the zot config to it.
func (zm *ZotManager) WriteTempZotConfig() (string, error) {
	zm.log.Debug().Msg("Creating temporary zot config file")

	tmpFile, err := os.CreateTemp("", "zot-*.json")
	if err != nil {
		zm.log.Error().Err(err).Msg("Failed to create temporary zot config file")
		return "", fmt.Errorf("failed to create temp zot config file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(zm.zotConfig); err != nil {
		zm.log.Error().Err(err).Str("path", tmpFile.Name()).Msg("Failed to write to temp zot config file")
		return "", fmt.Errorf("failed to write to temp zot file present at %s: %w", tmpFile.Name(), err)
	}

	zm.log.Debug().Str("path", tmpFile.Name()).Msg("Temporary zot config file created successfully")
	return tmpFile.Name(), nil
}

// RemoveTempZotConfig deletes the temporary zot config file.
func (zm *ZotManager) RemoveTempZotConfig(tmpPath string) error {
	zm.log.Debug().Str("path", tmpPath).Msg("Removing temp zot config file")

	if err := os.Remove(tmpPath); err != nil {
		zm.log.Error().Err(err).Str("path", tmpPath).Msg("Failed to delete temp zot config file")
		return fmt.Errorf("failed to delete temp zot_config file present at %s: %w", tmpPath, err)
	}

	zm.log.Debug().Str("path", tmpPath).Msg("Temp zot config file deleted successfully")
	return nil
}

// LaunchZotRegistry launches the zot registry using the given config path.
func (zm *ZotManager) LaunchZotRegistry(zotConfigPath string) error {
	zm.log.Info().Str("configPath", zotConfigPath).Msg("Launching zot registry")

	rootCmd := server.NewServerRootCmd()
	rootCmd.SetArgs([]string{"serve", zotConfigPath})

	if err := rootCmd.Execute(); err != nil {
		zm.log.Error().Err(err).Str("configPath", zotConfigPath).Msg("Failed to launch zot registry")
		return fmt.Errorf("failed to launch zot registry with config present at %s: %w", zotConfigPath, err)
	}

	zm.log.Info().Msg("Shutting down Zot registry")
	return nil
}

// ValidateRegistryConfig validates the zot registry configuration file.
func (zm *ZotManager) VerifyRegistryConfig(zotConfigPath string) error {
	zm.log.Info().Str("configPath", zotConfigPath).Msg("Validating zot config")

	rootCmd := server.NewServerRootCmd()
	rootCmd.SetArgs([]string{"verify", zotConfigPath})

	if err := rootCmd.Execute(); err != nil {
		zm.log.Error().Err(err).Str("configPath", zotConfigPath).Msg("Failed to validate zot config")
		return fmt.Errorf("failed to validate zot config at %s: %w", zotConfigPath, err)
	}

	zm.log.Info().Str("configPath", zotConfigPath).Msg("Zot config validated successfully")
	return nil
}
