package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

func CreateRuntimeDirectory(dir string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	runtimePath := filepath.Join(cwd, dir)
	// check if the runtime directory exists
	if _, err := os.Stat(runtimePath); os.IsNotExist(err) {
		// create the runtime directory
		err = os.MkdirAll(dir, 0o750)
		if err != nil {
			return fmt.Errorf("failed to create the runtime directory %s: %w", dir, err)
		}
	}
	return nil
}
