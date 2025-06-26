package e2e

import (
	"os"
	"path/filepath"
)

func getProjectRootDir() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return filepath.Abs(filepath.Join(currentDir, "../.."))
}
