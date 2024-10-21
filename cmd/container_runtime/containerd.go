package runtime

import (
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
	ContainerDCertPath       = "/etc/containerd/certs.d"
	DefaultGeneratedTomlName = "config.toml"
)

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
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			log := logger.FromContext(cmd.Context())
			if !generateConfig {
				return nil
			}
			log.Info().Msg("Generating containerd config file for containerd ...")
			return generateContainerdConfig(defaultZotConfig, log)
		},
	}

	containerdCmd.Flags().BoolVarP(&generateConfig, "gen", "g", false, "Generate the containerd config file")

	return containerdCmd
}

func generateContainerdConfig(defaultZotConfig *registry.DefaultZotConfig, log *zerolog.Logger) error {
	containerdConfig := containerd.Config{}
	containerdConfig.PluginConfig = containerd.DefaultConfig()
	containerdConfig.PluginConfig.Registry.ConfigPath = ContainerDCertPath

	registryMirror := map[string]containerd.Mirror{
		defaultZotConfig.HTTP.Address: {
			Endpoints: []string{defaultZotConfig.HTTP.Address + ":" + defaultZotConfig.HTTP.Port},
		},
	}

	registryConfig := map[string]containerd.RegistryConfig{
		defaultZotConfig.HTTP.Address: {
			TLS: &containerd.TLSConfig{
				InsecureSkipVerify: config.UseUnsecure(),
			},
		},
	}

	containerdConfig.PluginConfig.Registry.Mirrors = registryMirror
	containerdConfig.PluginConfig.Registry.Configs = registryConfig

	generatedConfig, err := toml.Marshal(containerdConfig)
	if err != nil {
		log.Error().Err(err).Msg("Error marshalling config")
		return fmt.Errorf("could not marshal config: %w", err)
	}

	filePath := filepath.Join(DefaultGenPath, DefaultGeneratedTomlName)
	log.Info().Msgf("Writing config to file: %s", filePath)
	file, err := os.Create(filePath)
	if err != nil {
		log.Err(err).Msg("Error creating file")
		return fmt.Errorf("could not create file: %w", err)
	}
	defer file.Close()

	_, err = file.Write(generatedConfig)
	if err != nil {
		log.Err(err).Msg("Error writing to file")
		return fmt.Errorf("could not write to file: %w", err)
	}

	log.Info().Msgf("Config file generated successfully at: %s", filePath)
	return nil
}
