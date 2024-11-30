package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
)

const (
	SatelliteConfigPath    = "satellite.io"
	HostToml              = "host_gen.toml"
	DefaultTomlConfigPath = "_default"
	DockerURL             = "docker.io"
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
	mirrorGenPath := fmt.Sprintf("%s/%s", genPath, SatelliteConfigPath)
	err := utils.CreateRuntimeDirectory(mirrorGenPath)
	if err != nil {
		log.Err(err).Msgf("Error creating the directory: %s", mirrorGenPath)
		return fmt.Errorf("error creating the directory: %v", err)
	}
	satelliteHubHostConfigPath := fmt.Sprintf("%s/%s/%s", containerdCertPath, SatelliteConfigPath, HostToml)
	var satelliteContainerdHostConfig ContainerdHostConfig

	// Read the `satellite/host.toml` file if present
	data, err := utils.ReadFile(satelliteHubHostConfigPath, false)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Msgf("The satellite/host.toml file does not exist at path: %s", satelliteHubHostConfigPath)
		} else {
			return fmt.Errorf("error reading the satellite/host.toml file: %v", err)
		}
	}
	err = toml.Unmarshal(data, &satelliteContainerdHostConfig)
	if err != nil {
		log.Err(err).Msgf("Error unmarshalling the satellite/host.toml file at path: %s", satelliteHubHostConfigPath)
		return fmt.Errorf("error unmarshalling the satellite/host.toml file: %v", err)
	}
	satelliteHostConfigToAdd := ContainerdHostConfig{
		Host: map[string]HostConfig{
			satelliteHostConfig.LocalRegistry: {
				Capabilities: []string{"pull", "push", "resolve"},
				SkipVerify:   config.UseUnsecure(),
			},
		},
	}

	if satelliteContainerdHostConfig.Server == "" {
		satelliteContainerdHostConfig.Server = DockerURL
	}
	if satelliteContainerdHostConfig.Host == nil {
		satelliteContainerdHostConfig.Host = satelliteHostConfigToAdd.Host
	} else {
		for key, value := range satelliteContainerdHostConfig.Host {
			satelliteHostConfigToAdd.Host[key] = value
		}
		satelliteContainerdHostConfig.Host = satelliteHostConfigToAdd.Host
	}

	pathTOWrite := filepath.Join(mirrorGenPath, HostToml)
	log.Info().Msgf("Writing the host.toml file at path: %s", pathTOWrite)
	hostData, err := toml.Marshal(satelliteContainerdHostConfig)
	if err != nil {
		log.Err(err).Msg("Error marshalling the host.toml file")
		return fmt.Errorf("error marshalling the host.toml file: %v", err)
	}
	hostStr := string(hostData)
	hostStr = strings.Replace(hostStr, "[host]\n", "", 1)
	hostData = []byte(hostStr)
	err = utils.WriteFile(pathTOWrite, hostData)
	if err != nil {
		log.Err(err).Msg("Error writing the host.toml file")
		return fmt.Errorf("error writing the host.toml file: %v", err)
	}
	log.Info().Msg("Successfully wrote the host.toml file")
	return nil
}
