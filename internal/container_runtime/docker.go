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

func setDockerdConfig(mirrors []string, localRegistry string) (string, error) {
	if len(mirrors) == 0 {
		return "", nil
	}
	enabled, err := strconv.ParseBool(mirrors[0])
	if err != nil {
		return "", fmt.Errorf("docker mirror must be true/false, got %q", mirrors[0])
	}
	if !enabled {
		return "", nil
	}

	// validate URI
	if !strings.HasPrefix(localRegistry, "http://") && !strings.HasPrefix(localRegistry, "https://") {
		localRegistry = "http://" + localRegistry
	}

	backupPath, err := backupFile(dockerConfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to backup docker config: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(dockerConfigPath)
	v.SetConfigType("json")

	if err := ensureDockerConfigFileExists(dockerConfigPath); err != nil {
		return backupPath, fmt.Errorf("failed to create default docker config : %w", err)
	}

	if err := v.ReadInConfig(); err != nil {
		return backupPath, fmt.Errorf("failed to read docker config : %w", err)
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
		return backupPath, fmt.Errorf("failed to write docker config: %w", err)
	}

	// validate written config
	data, err := os.ReadFile(dockerConfigPath)
	if err != nil {
		return backupPath, fmt.Errorf("failed to read back docker config: %w", err)
	}
	if err := validateJSON(data); err != nil {
		if backupPath != "" {
			_ = restoreBackup(backupPath, dockerConfigPath)
		}
		return backupPath, fmt.Errorf("docker config validation failed, rolled back: %w", err)
	}

	// restart docker safely
	cmd := exec.Command("systemctl", "restart", "docker")
	if err := cmd.Run(); err != nil {
		if backupPath != "" {
			_ = restoreBackup(backupPath, dockerConfigPath)
		}
		return backupPath, fmt.Errorf("failed to restart Docker, rolled back config: %w", err)
	}

	return backupPath, nil
}

func ensureDockerConfigFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// create the file with {}
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			return err
		}
	}
	return nil
}
