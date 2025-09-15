package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func setDockerdConfig(mirror string, localRegistry string) error {
	enabled, err := strconv.ParseBool(mirror)
	if err != nil {
		return fmt.Errorf("docker mirror must be true/false, got %q", mirror)
	}
	if enabled {
		const dockerConfigPath = "/etc/docker/daemon.json"

		config := make(map[string]interface{})

		if data, err := os.ReadFile(dockerConfigPath); err == nil {
			_ = json.Unmarshal(data, &config)
		}

		// update dockerd config while preserving existing settings
		if _, ok := config["registry-mirrors"]; !ok {
			config["registry-mirrors"] = []string{localRegistry}
			newData, _ := json.MarshalIndent(config, "", "  ")
			_ = os.WriteFile(dockerConfigPath, newData, 0644)
		}

		// safely restart docker service
		cmd := exec.Command("systemctl", "restart", "docker")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error: failed to restart Docker : %w", err)
		}
	}
	return nil
}
