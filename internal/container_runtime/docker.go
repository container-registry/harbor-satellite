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

	// Ensure localRegistry has a valid scheme
	if !strings.HasPrefix(localRegistry, "http://") && !strings.HasPrefix(localRegistry, "https://") {
		localRegistry = "http://" + localRegistry
	}

	config := make(map[string]interface{})
	if data, err := os.ReadFile(dockerConfigPath); err == nil {
		err = json.Unmarshal(data, &config)
		if err != nil {
			return fmt.Errorf("failed to unmarshal docker config: %w", err)
		}
	}

	config["registry-mirrors"] = []string{localRegistry}

	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal docker config: %w", err)
	}

	if err := os.WriteFile(dockerConfigPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write docker config: %w", err)
	}

	cmd := exec.Command("systemctl", "restart", "docker")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart Docker: %w", err)
	}

	return nil
}
