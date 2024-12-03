package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"container-registry.com/harbor-satellite/registry"
	toml "github.com/pelletier/go-toml"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

const (
	ContainerDCertPath          = "/etc/containerd/certs.d"
	DefaultGeneratedTomlName    = "config.toml"
	ContainerdRuntime           = "containerd"
	DefaultContainerdConfigPath = "/etc/containerd/config.toml"
	DefaultConfigVersion        = 2
)

var DefaultContainerDGenPath string

func init() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current working directory: %v\n", err)
		DefaultContainerDGenPath = "/runtime/containerd"
		if _, err := os.Stat(DefaultContainerDGenPath); os.IsNotExist(err) {
			err := os.MkdirAll(DefaultContainerDGenPath, os.ModePerm)
			if err != nil {
				fmt.Printf("Error creating default directory: %v\n", err)
			}
		}
	} else {
		DefaultContainerDGenPath = filepath.Join(cwd, "runtime/containerd")
	}
}

func NewContainerdCommand() *cobra.Command {
	var generateConfig bool
	var defaultZotConfig registry.DefaultZotConfig
	var containerdConfigPath string
	var containerDCertPath string

	containerdCmd := &cobra.Command{
		Use:   "containerd",
		Short: "Creates the config file for the containerd runtime to fetch the images from the local repository",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return SetupContainerRuntimeCommand(cmd, &defaultZotConfig, DefaultContainerDGenPath)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			log := logger.FromContext(cmd.Context())
			sourceRegistry := config.GetSourceRegistryURL()
			satelliteHostConfig := NewSatelliteHostConfig(defaultZotConfig.RemoteURL, sourceRegistry)
			if generateConfig {
				log.Info().Msg("Generating containerd config file for containerd ...")
				log.Info().Msgf("Fetching containerd config from path: %s", containerdConfigPath)
				err := GenerateContainerdHostConfig(containerDCertPath, DefaultContainerDGenPath, log, *satelliteHostConfig)
				if err != nil {
					log.Err(err).Msg("Error generating containerd config")
					return fmt.Errorf("could not generate containerd config: %w", err)
				}
				return GenerateContainerdConfig(log, containerdConfigPath, containerDCertPath)
			}
			return nil
		},
	}

	containerdCmd.Flags().BoolVarP(&generateConfig, "gen", "g", false, "Generate the containerd config file")
	containerdCmd.PersistentFlags().StringVarP(&containerdConfigPath, "path", "p", DefaultContainerdConfigPath, "Path to the containerd config file of the container runtime")
	containerdCmd.PersistentFlags().StringVarP(&containerDCertPath, "cert-path", "c", ContainerDCertPath, "Path to the containerd cert directory")
	containerdCmd.AddCommand(NewReadConfigCommand(ContainerdRuntime))
	return containerdCmd
}

// GenerateContainerdConfig generates the containerd config file for the containerd runtime
// It takes the zot config a logger and the containerd config path
// It reads the containerd config file and adds the local registry to the config file
func GenerateContainerdConfig(log *zerolog.Logger, containerdConfigPath, containerdCertPath string) error {
	// First Read the present config file at the configPath
	data, err := utils.ReadFile(containerdConfigPath, false)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Msg("Config file does not exist, proceeding with default values")
			data = []byte{}
		} else {
			log.Err(err).Msg("Error reading config file")
			return fmt.Errorf("could not read config file: %w", err)
		}
	}
	// Now we marshal the data into the containerd config
	containerdConfig := &ContainerdConfigToml{}
	err = toml.Unmarshal(data, containerdConfig)
	if err != nil {
		log.Err(err).Msg("Error unmarshalling config")
		return fmt.Errorf("could not unmarshal config: %w", err)
	}
	// Add the certs.d path to the config
	if containerdConfig.Plugins.Cri.Registry.ConfigPath == "" {
		containerdConfig.Plugins.Cri.Registry.ConfigPath = containerdCertPath
	}
	// Set default version
	if containerdConfig.Version == 0 {
		containerdConfig.Version = DefaultConfigVersion
	}
	// if config disabled plugins container cri then remove it
	if len(containerdConfig.DisabledPlugins) > 0 {
		filteredPlugins := make([]string, 0, len(containerdConfig.DisabledPlugins))
		for _, plugin := range containerdConfig.DisabledPlugins {
			if plugin != "cri" {
				filteredPlugins = append(filteredPlugins, plugin)
			}
		}
		containerdConfig.DisabledPlugins = filteredPlugins
	}
	// ToDo: Find a way to remove the unwanted configuration added to the config file while marshalling
	pathToWrite := filepath.Join(DefaultContainerDGenPath, DefaultGeneratedTomlName)
	log.Info().Msgf("Writing the containerd config to path: %s", pathToWrite)
	// Now we write the config to the file
	data, err = toml.Marshal(containerdConfig)
	dataStr := string(data)
	dataStr = strings.Replace(dataStr, "[plugins]\n", "", 1)
	data = []byte(dataStr)
	if err != nil {
		log.Err(err).Msg("Error marshalling config")
		return fmt.Errorf("could not marshal config: %w", err)
	}
	err = utils.WriteFile(pathToWrite, data)
	if err != nil {
		log.Err(err).Msg("Error writing config to file")
		return fmt.Errorf("could not write config to file: %w", err)
	}
	return nil
}
