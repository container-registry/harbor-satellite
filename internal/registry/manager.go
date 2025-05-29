package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
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

func (zm *ZotManager) HandleRegistrySetup(g *errgroup.Group, ctx context.Context) error {
	tmpConfigPath, err := zm.WriteTempZotConfig()
	if err != nil {
		zm.log.Error().Err(err).Msg("Error writing temp zot config to disk")
		return fmt.Errorf("error writing temp zot config to disk: %w", err)
	}

	errChan := make(chan error, 1)

	shutDownChan := make(chan struct{}, 1)

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
			return fmt.Errorf("error launching default zot registry: %w", err)
		}

		zm.LaunchZotRegistry(ctx, tmpConfigPath, shutDownChan, errChan)

		select {
		case <-ctx.Done():
			zm.log.Warn().Msg("Context cancelled, zot registry will be terminated")
			return ctx.Err()

		case err := <-errChan:
			if err != nil {
				zm.log.Error().Err(err).Msg("Error launching default zot registry")
				return fmt.Errorf("error launching default zot registry: %w", err)
			}

		case <-shutDownChan:
			zm.log.Warn().Err(err).Msg("Zot registry received a shutdown signal, shutting it down...")
			return fmt.Errorf("zot registry is shutting down due to a shut down signal")
		}

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

	if _, err := tmpFile.Write(zm.zotConfig); err != nil {
		zm.log.Error().Err(err).Str("path", tmpFile.Name()).Msg("Failed to write to temp zot config file")
		return "", fmt.Errorf("failed to write to temp zot file present at %s: %w", tmpFile.Name(), err)
	}

	if err := tmpFile.Close(); err != nil {
		zm.log.Error().Err(err).Str("path", tmpFile.Name()).Msg("Failed to close temp zot config file")
		return "", fmt.Errorf("failed to close temp zot config file %s: %w", tmpFile.Name(), err)
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

// LaunchZotRegistry launches the zot registry by running `zot serve {zotConfigPath}` in a go routine.
func (zm *ZotManager) LaunchZotRegistry(ctx context.Context, zotConfigPath string, shutdownChan chan struct{}, errChan chan error) {
	zm.log.Info().Str("configPath", zotConfigPath).Msg("Launching zot registry")

	rootCmd := server.NewServerRootCmd()
	rootCmd.SetArgs([]string{"serve", zotConfigPath})

	go func() {
		err := rootCmd.Execute()
		if err != nil {
			errChan <- fmt.Errorf("failed to launch zot registry with config present at %s: %w", zotConfigPath, err)
		} else {
			shutdownChan <- struct{}{}
		}
	}()
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
