package runtime

import (
	"fmt"
	"path/filepath"

	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"github.com/spf13/cobra"
)

var (
	DefaultContainerdConfigPath string = filepath.Join("/", "etc/containerd/config.toml")
)

func NewReadConfigCommand(runtime string) *cobra.Command {
	var defaultPath string
	switch runtime {
	case "containerd":
		defaultPath = DefaultContainerdConfigPath
	default:
		defaultPath = ""
	}
	readContainerdConfig := &cobra.Command{
		Use:   "read",
		Short: fmt.Sprintf("Reads the config file for the %s runtime", runtime),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
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
			err = utils.ReadFile(path)
			if err != nil {
				return fmt.Errorf("error reading the containerd config file: %v", err)
			}
			return nil
		},
	}
	readContainerdConfig.Flags().StringP("path", "p", defaultPath, "Path to the containerd config file of the container runtime")
	return readContainerdConfig
}
