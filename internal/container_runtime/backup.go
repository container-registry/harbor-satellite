package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// backupFile creates a timestamped backup of the given file.
// Returns the backup path, or empty string if the source does not exist.
func backupFile(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	}

	timestamp := time.Now().Format("20060102T150405")
	backupPath := fmt.Sprintf("%s.bak.%s", path, timestamp)

	if err := copyFile(path, backupPath); err != nil {
		return "", fmt.Errorf("backup %s: %w", path, err)
	}

	return backupPath, nil
}

// validateJSON checks whether data is valid JSON.
func validateJSON(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON content")
	}
	return nil
}

// validateTOML checks whether data is valid TOML.
func validateTOML(data []byte) error {
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("invalid TOML content: %w", err)
	}
	return nil
}

// restoreBackup copies a backup file back to the original path.
func restoreBackup(backupPath, originalPath string) error {
	return copyFile(backupPath, originalPath)
}
