package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

const dockerConfigPath = "/etc/docker/daemon.json"

func setDockerdConfig(mirrors []string, localRegistry string) error {
	if len(mirrors) == 0 {
		return nil
	}

	flag := mirrors[0]
	enabled, err := strconv.ParseBool(flag)
	if err != nil {
		return fmt.Errorf("docker mirror must be true/false, got %q", flag)
	}

	if !enabled {
		return nil
	}

	config := make(map[string]interface{})
	if data, err := os.ReadFile(dockerConfigPath); err == nil {
		_ = json.Unmarshal(data, &config)
	}

	// Update dockerd config while preserving existing settings
	if _, ok := config["registry-mirrors"]; !ok {
		config["registry-mirrors"] = []string{localRegistry}
		newData, _ := json.MarshalIndent(config, "", "  ")
		_ = os.WriteFile(dockerConfigPath, newData, 0644)
	}

	cmd := exec.Command("systemctl", "restart", "docker")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart Docker: %w", err)
	}

	return nil
}
