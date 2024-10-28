package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"container-registry.com/harbor-satellite/registry"
	containerd "github.com/containerd/containerd/pkg/cri/config"
	toml "github.com/pelletier/go-toml"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

const (
	ContainerDCertPath          = "/etc/containerd/certs.d"
	DefaultGeneratedTomlName    = "config.toml"
	ContainerdRuntime           = "containerd"
	DefaultContainerdConfigPath = "/etc/containerd/config.toml"
)

type ContainerdController interface {
	Load(ctx context.Context, log *zerolog.Logger) (*registry.DefaultZotConfig, error)
	Generate(ctx context.Context, configPath string, log *zerolog.Logger) error
}

var DefaultGenPath string

func init() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current working directory: %v\n", err)
		DefaultGenPath = "/runtime/containerd" // Fallback in case of error
	} else {
		DefaultGenPath = filepath.Join(cwd, "runtime/containerd")
	}
}

func NewContainerdCommand() *cobra.Command {
	var generateConfig bool
	var defaultZotConfig *registry.DefaultZotConfig
	var containerdConfigPath string
	var containerDCertPath string

	containerdCmd := &cobra.Command{
		Use:   "containerd",
		Short: "Creates the config file for the containerd runtime to fetch the images from the local repository",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			utils.SetupContextForCommand(cmd)
			log := logger.FromContext(cmd.Context())
			if config.GetOwnRegistry() {
				log.Info().Msg("Using own registry for config generation")
				address, err := utils.ValidateRegistryAddress(config.GetOwnRegistryAdr(), config.GetOwnRegistryPort())
				if err != nil {
					log.Err(err).Msg("Error validating registry address")
					return err
				}
				log.Info().Msgf("Registry address validated: %s", address)
				defaultZotConfig.HTTP.Address = config.GetOwnRegistryAdr()
				defaultZotConfig.HTTP.Port = config.GetOwnRegistryPort()
			} else {
				log.Info().Msg("Using default registry for config generation")
				defaultZotConfig, err = registry.ReadConfig(config.GetZotConfigPath())
				if err != nil {
					return fmt.Errorf("could not read config: %w", err)
				}
				log.Info().Msgf("Default config read successfully: %v", defaultZotConfig.HTTP.Address+":"+defaultZotConfig.HTTP.Port)
			}
			return utils.CreateRuntimeDirectory(DefaultGenPath)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			log := logger.FromContext(cmd.Context())
			sourceRegistry := config.GetRemoteRegistryURL()
			satelliteHostConfig := NewSatelliteHostConfig(defaultZotConfig.GetLocalRegistryURL(), sourceRegistry)
			if generateConfig {
				log.Info().Msg("Generating containerd config file for containerd ...")
				log.Info().Msgf("Fetching containerd config from path path: %s", containerdConfigPath)
				return GenerateContainerdHostConfig(containerDCertPath, DefaultGenPath, log, *satelliteHostConfig)
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

// GenerateConfig generates the containerd config file for the containerd runtime
// It takes the zot config a logger and the containerd config path
// It reads the containerd config file and adds the local registry to the config file
func GenerateConfig(defaultZotConfig *registry.DefaultZotConfig, log *zerolog.Logger, containerdConfigPath, containerdCertPath string) error {
	// First Read the present config file at the configPath
	data, err := utils.ReadFile(containerdConfigPath, false)
	if err != nil {
		log.Err(err).Msg("Error reading config file")
		return fmt.Errorf("could not read config file: %w", err)
	}
	// Now we marshal the data into the containerd config
	containerdConfig := &containerd.Config{}
	err = toml.Unmarshal(data, containerdConfig)
	if err != nil {
		log.Err(err).Msg("Error unmarshalling config")
		return fmt.Errorf("could not unmarshal config: %w", err)
	}
	// Steps to configure the containerd config:
	// 1. Set the default registry config cert path
	//  -- This is the path where the certs of the registry are stored
	//  -- If the user has already has a cert path then we do not set it rather we would now use the
	//     user path as the default path
	if containerdConfig.PluginConfig.Registry.ConfigPath == "" {
		containerdConfig.PluginConfig.Registry.ConfigPath = containerdCertPath
	}
	log.Info().Msgf("Setting the registry cert path to: %s", containerdConfig.PluginConfig.Registry.ConfigPath)
	// Now we add the local registry to the containerd config mirrors
	registryMirror := map[string]containerd.Mirror{
		defaultZotConfig.HTTP.Address: {
			Endpoints: []string{defaultZotConfig.HTTP.Address + ":" + defaultZotConfig.HTTP.Port},
		},
	}
	if containerdConfig.PluginConfig.Registry.Mirrors == nil {
		containerdConfig.PluginConfig.Registry.Mirrors = registryMirror
	} else {
		for key, value := range registryMirror {
			containerdConfig.PluginConfig.Registry.Mirrors[key] = value
		}
	}
	registryConfig := map[string]containerd.RegistryConfig{
		defaultZotConfig.HTTP.Address: {
			TLS: &containerd.TLSConfig{
				InsecureSkipVerify: config.UseUnsecure(),
			},
		},
	}
	// Now we add the local registry to the containerd config registry
	if containerdConfig.PluginConfig.Registry.Configs == nil {
		containerdConfig.PluginConfig.Registry.Configs = registryConfig
	} else {
		for key, value := range registryConfig {
			containerdConfig.PluginConfig.Registry.Configs[key] = value
		}
	}
	// ToDo: Find a way to remove the unwanted configuration added to the config file while marshalling
	pathToWrite := filepath.Join(DefaultGenPath, DefaultGeneratedTomlName)
	log.Info().Msgf("Writing the containerd config to path: %s", pathToWrite)
	// Now we write the config to the file
	data, err = toml.Marshal(containerdConfig)
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