package runtime

import (
	"fmt"
	"os"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
)

const (
	DockerIoConfigPath    = "docker"
	HostToml              = "host_gen.toml"
	DefaultTomlConfigPath = "_default"
	DockerURL             = "https://registry-1.docker.io"
)

type ContainerdHostConfig struct {
	Server string                `toml:"server,omitempty"`
	Host   map[string]HostConfig `toml:"host,omitempty"`
}

type HostConfig struct {
	Capabilities []string            `toml:"capabilities,omitempty"`
	CA           interface{}         `toml:"ca,omitempty"`
	Client       interface{}         `toml:"client,omitempty"`
	SkipVerify   bool                `toml:"skip_verify,omitempty"`
	Header       map[string][]string `toml:"header,omitempty"`
	OverridePath bool                `toml:"override_path,omitempty"`
}

type SatelliteHostConfig struct {
	LocalRegistry  string
	SourceRegistry string
}

func NewSatelliteHostConfig(localRegistry, sourceRegistry string) *SatelliteHostConfig {
	return &SatelliteHostConfig{
		LocalRegistry:  localRegistry,
		SourceRegistry: sourceRegistry,
	}
}

// GenerateContainerdHostConfig generates the host.toml file for containerd docker.io and also create a default config.toml file
func GenerateContainerdHostConfig(containerdCertPath, genPath string, log *zerolog.Logger, satelliteHostConfig SatelliteHostConfig) error {
	mirrorGenPath := fmt.Sprintf("%s/%s", genPath, DockerIoConfigPath)
	err := utils.CreateRuntimeDirectory(mirrorGenPath)
	if err != nil {
		log.Err(err).Msgf("Error creating the directory: %s", mirrorGenPath)
		return fmt.Errorf("error creating the directory: %v", err)
	}
	dockerHubHostConfigPath := fmt.Sprintf("%s/%s/%s", containerdCertPath, DockerIoConfigPath, HostToml)
	var dockerContainerdHostConfig ContainerdHostConfig

	// Read the `docker.io/host.toml` file if present
	data, err := utils.ReadFile(dockerHubHostConfigPath, false)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Msgf("The docker.io/host.toml file does not exist at path: %s", dockerHubHostConfigPath)
		} else {
			return fmt.Errorf("error reading the docker.io/host.toml file: %v", err)
		}
	}
	err = toml.Unmarshal(data, &dockerContainerdHostConfig)
	if err != nil {
		log.Err(err).Msgf("Error unmarshalling the docker.io/host.toml file at path: %s", dockerHubHostConfigPath)
		return fmt.Errorf("error unmarshalling the docker.io/host.toml file: %v", err)
	}
	satelliteHostConfigToAdd := ContainerdHostConfig{
		Host: map[string]HostConfig{
			satelliteHostConfig.LocalRegistry: {
				Capabilities: []string{"pull", "push", "resolve"},
				SkipVerify:   config.UseUnsecure(),
			},
		},
	}

	if dockerContainerdHostConfig.Server == "" {
		dockerContainerdHostConfig.Server = DockerURL
	}
	if dockerContainerdHostConfig.Host == nil {
		dockerContainerdHostConfig.Host = satelliteHostConfigToAdd.Host
	} else {
		for key, value := range dockerContainerdHostConfig.Host {
			satelliteHostConfigToAdd.Host[key] = value
		}
		dockerContainerdHostConfig.Host = satelliteHostConfigToAdd.Host
	}

	pathTOWrite := fmt.Sprintf("%s/%s", mirrorGenPath, HostToml)
	log.Info().Msgf("Writing the host.toml file at path: %s", pathTOWrite)
	hostData, err := toml.Marshal(dockerContainerdHostConfig)
	if err != nil {
		log.Err(err).Msg("Error marshalling the host.toml file")
		return fmt.Errorf("error marshalling the host.toml file: %v", err)
	}
	err = utils.WriteFile(pathTOWrite, hostData)
	if err != nil {
		log.Err(err).Msg("Error writing the host.toml file")
		return fmt.Errorf("error writing the host.toml file: %v", err)
	}
	log.Info().Msg("Successfully wrote the host.toml file")
	return nil
}
