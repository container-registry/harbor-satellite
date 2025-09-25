package runtime

import (
	"fmt"
	"github.com/spf13/viper"
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

	v := viper.New()
	v.SetConfigFile(dockerConfigPath)
	v.SetConfigType("json")

	if err := ensureDockerConfigFileExists(dockerConfigPath); err != nil {
		return fmt.Errorf("failed to create default docker config : %w", err)
	}

	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read docker config : %w", err)
	}
	currentMirrors := v.GetStringSlice("registry-mirrors")

	// Append the new mirror if not present
	found := false
	for _, m := range currentMirrors {
		if m == localRegistry {
			found = true
			break
		}
	}
	if !found {
		currentMirrors = append(currentMirrors, localRegistry)
		v.Set("registry-mirrors", currentMirrors)
	}

	if err := v.WriteConfigAs(dockerConfigPath); err != nil {
		return fmt.Errorf("failed to write docker config: %w", err)
	}

	// restart docker safely
	cmd := exec.Command("systemctl", "restart", "docker")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart Docker: %w", err)
	}

	return nil
}

func ensureDockerConfigFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// create the file with {}
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			return err
		}
	}
	return nil
}
