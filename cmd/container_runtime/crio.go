package runtime

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"container-registry.com/harbor-satellite/registry"
	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

const (
	DefaultCrioRegistryConfigPath = "/etc/containers/registries.conf.d/crio.conf"
)

var DefaultCrioGenPath string

func init() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current working directory: %v\n", err)
		if _, err := os.Stat(DefaultCrioGenPath); os.IsNotExist(err) {
			DefaultCrioGenPath = "runtime/crio"
			err := os.MkdirAll(DefaultCrioGenPath, os.ModePerm)
			if err != nil {
				fmt.Printf("Error creating default directory: %v\n", err)
			}
		}
	} else {
		DefaultCrioGenPath = filepath.Join(cwd, "runtime/crio")
	}
}

func NewCrioCommand() *cobra.Command {
	var defaultZotConfig registry.DefaultZotConfig
	var generateConfig bool
	var crioConfigPath string

	crioCmd := &cobra.Command{
		Use:   "crio",
		Short: "Creates the config file for the crio runtime to fetch the images from the local repository",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return SetupContainerRuntimeCommand(cmd, &defaultZotConfig, DefaultCrioGenPath)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			log := logger.FromContext(cmd.Context())
			if generateConfig {
				log.Info().Msg("Generating the config file for crio ...")
				log.Info().Msgf("Fetching crio registry config file form path: %s", crioConfigPath)
				err := GenerateCrioRegistryConfig(&defaultZotConfig, crioConfigPath, log)
				if err != nil {
					log.Err(err).Msg("Error generating crio registry config")
					return err
				}
			}
			return nil
		},
	}
	crioCmd.Flags().BoolVarP(&generateConfig, "gen", "g", false, "Generate the config file")
	crioCmd.PersistentFlags().StringVarP(&crioConfigPath, "config", "c", DefaultCrioRegistryConfigPath, "Path to the crio registry config file")
	return crioCmd
}

func GenerateCrioRegistryConfig(defaultZotConfig *registry.DefaultZotConfig, crioConfigPath string, log *zerolog.Logger) error {
	// Read the current crio registry config file
	data, err := utils.ReadFile(crioConfigPath, false)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Msg("Config file does not exist, proceeding with default values")
			data = []byte{}
		} else {
			log.Err(err).Msg("Error reading config file")
			return fmt.Errorf("could not read config file: %w", err)
		}
	}
	var crioRegistryConfig CriORegistryConfig
	err = toml.Unmarshal(data, &crioRegistryConfig)
	if err != nil {
		log.Err(err).Msg("Error unmarshalling crio registry config")
		return fmt.Errorf("could not unmarshal crio registry config: %w", err)
	}
	// Update the crio registry config file
	// - Add the local registry to the unqualified search registries if not already present
	var found bool = false
	var localRegistry string = utils.FormatRegistryURL(defaultZotConfig.RemoteURL)
	for _, registry := range crioRegistryConfig.UnqualifiedSearchRegistries {
		if registry == localRegistry {
			found = true
			break
		}
	}
	if !found {
		crioRegistryConfig.UnqualifiedSearchRegistries = append(crioRegistryConfig.UnqualifiedSearchRegistries, localRegistry)
	}
	// Now range over the registries and find if there is a registry with the prefix satellite
	// If there is a registry with the prefix satellite, update the location to the local registry
	found = false
	for _, registries := range crioRegistryConfig.Registries {
		if registries.Prefix == "satellite.io" {
			found = true
			if registries.Location == "" {
				registries.Location = DockerURL
			}
			// Add the local registry to the first position in the mirrors
			mirror := Mirror{
				Location: localRegistry,
				Insecure: config.UseUnsecure(),
			}
			registries.Mirrors = append([]Mirror{mirror}, registries.Mirrors...)
		}
	}
	if !found {
		// Add the satellite registry to the registries
		registry := Registry{
			Prefix:   "satellite.io",
			Location: DockerURL,
			Mirrors: []Mirror{
				{
					Location: localRegistry,
					Insecure: config.UseUnsecure(),
				},
			},
		}
		crioRegistryConfig.Registries = append(crioRegistryConfig.Registries, registry)
	}
	// Now marshal the updated crio registry config
	updatedData, err := toml.Marshal(crioRegistryConfig)
	if err != nil {
		log.Err(err).Msg("Error marshalling crio registry config")
		return fmt.Errorf("could not marshal crio registry config: %w", err)
	}
	// Write the updated crio registry config to the file
	pathToWrite := filepath.Join(DefaultCrioGenPath, "crio.conf")
	log.Info().Msgf("Writing the crio registry config to path: %s", pathToWrite)
	err = utils.WriteFile(pathToWrite, updatedData)
	if err != nil {
		log.Err(err).Msg("Error writing crio registry config")
		return fmt.Errorf("could not write crio registry config: %w", err)
	}
	log.Info().Msg("Successfully wrote the crio registry config")
	return nil
}

func SetupContainerRuntimeCommand(cmd *cobra.Command, defaultZotConfig *registry.DefaultZotConfig, defaultGenPath string) error {
	utils.CommandRunSetup(cmd)
	var err error
	log := logger.FromContext(cmd.Context())

	if config.GetOwnRegistry() {
		log.Info().Msg("Using own registry for config generation")
		log.Info().Msgf("Remote registry URL: %s", config.GetRemoteRegistryURL())
		_, err := url.Parse(config.GetRemoteRegistryURL())
		if err != nil {
			return fmt.Errorf("could not parse remote registry URL: %w", err)
		}
		defaultZotConfig.RemoteURL = config.GetRemoteRegistryURL()
	} else {
		log.Info().Msg("Using default registry for config generation")
		err = registry.ReadConfig(config.GetZotConfigPath(), defaultZotConfig)
		if err != nil || defaultZotConfig == nil {
			return fmt.Errorf("could not read config: %w", err)
		}
		defaultZotConfig.RemoteURL = defaultZotConfig.GetLocalRegistryURL()
		log.Info().Msgf("Default config read successfully: %v", defaultZotConfig.HTTP.Address+":"+defaultZotConfig.HTTP.Port)
	}
	return utils.CreateRuntimeDirectory(defaultGenPath)
}
