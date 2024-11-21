package runtime

import (
	"fmt"
	"os"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"github.com/spf13/cobra"
)

func NewReadConfigCommand(runtime string) *cobra.Command {
	readContainerdConfig := &cobra.Command{
		Use:   "read",
		Short: fmt.Sprintf("Reads the config file for the %s runtime", runtime),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			checks, warnings := config.InitConfig(config.DefaultConfigPath)
			if len(checks) > 0 || len(warnings) > 0 {
				ctx := cmd.Context()
				ctx, cancel := utils.SetupContext(ctx)
				ctx = logger.AddLoggerToContext(ctx, "info")
				log := logger.FromContext(ctx)
				for _, warn := range warnings {
					log.Warn().Msg(warn)
				}
				for _, err := range checks {
					log.Error().Err(err).Msg("Error initializing config")
				}
				cancel()
				os.Exit(1)
			}
			utils.SetupContextForCommand(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			//Parse the flags
			path, err := cmd.Flags().GetString("path")
			if err != nil {
				return fmt.Errorf("error reading the path flag: %v", err)
			}
			log := logger.FromContext(cmd.Context())
			log.Info().Msgf("Reading the containerd config file from path: %s", path)
			_, err = utils.ReadFile(path, true)
			if err != nil {
				return fmt.Errorf("error reading the containerd config file: %v", err)
			}
			return nil
		},
	}
	return readContainerdConfig
}
