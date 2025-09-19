package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const dockerConfigPath = "/etc/docker/daemon.json"

type DockerConfig struct {
	RegistryMirrors []string `json:"registry-mirrors,omitempty"`
}

func setDockerdConfig(mirrors []string, localRegistry string) error {
	if len(mirrors) == 0 {
		return nil
	}

	enabled, err := strconv.ParseBool(mirrors[0])
	if err != nil {
		return fmt.Errorf("docker mirror must be true/false, got %q", mirrors[0])
	}
	if !enabled {
		return nil
	}

	// validate URI
	if !strings.HasPrefix(localRegistry, "http://") && !strings.HasPrefix(localRegistry, "https://") {
		localRegistry = "http://" + localRegistry
	}

	var config DockerConfig
	if data, err := os.ReadFile(dockerConfigPath); err == nil {
		_ = json.Unmarshal(data, &config)
	}

	// Add mirror if not already in list
	alreadyPresent := false
	for _, m := range config.RegistryMirrors {
		if m == localRegistry {
			alreadyPresent = true
			break
		}
	}
	if !alreadyPresent {
		config.RegistryMirrors = append(config.RegistryMirrors, localRegistry)
	}

	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal docker config: %w", err)
	}
	if err := os.WriteFile(dockerConfigPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write docker config: %w", err)
	}

	//restart docker service safely
	cmd := exec.Command("systemctl", "restart", "docker")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart Docker: %w", err)
	}

	return nil
}
