package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathConfig holds all resolved file paths for satellite storage.
type PathConfig struct {
	ConfigDir        string
	ConfigFile       string
	PrevConfigFile   string
	StoreDir         string
	StateFile        string
	DesiredStateFile string
}

// expandPath expands ~ and ~/ to the user's home directory in paths.
func expandPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, path[2:]), nil
}

// ensureDir creates the directory if it doesn't exist and verifies it's writable.
func ensureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}

	// Verify writability
	testFile := filepath.Join(path, ".write-test")
	if err := os.WriteFile(testFile, []byte{}, 0o600); err != nil {
		return fmt.Errorf("directory %s not writable: %w", path, err)
	}
	if err := os.Remove(testFile); err != nil {
		return fmt.Errorf("clean up write test in %s: %w", path, err)
	}

	return nil
}

// DefaultConfigDir returns the default configuration directory.
// Uses XDG_CONFIG_HOME or falls back to ~/.config/satellite.
func DefaultConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get user config directory: %w", err)
	}

	return filepath.Join(configDir, "satellite"), nil
}

// defaultStoreDir picks the OCI store location for configDir. A non-root
// user gets $XDG_DATA_HOME/satellite/oci, falling back to
// ~/.local/share/satellite/oci; root gets /var/lib/satellite/oci. A relative
// XDG_DATA_HOME is ignored, as the XDG base directory spec asks. homeDir is
// called only for the fallback, so a missing home does not matter otherwise.
func defaultStoreDir(uid int, xdgDataHome string, homeDir func() (string, error)) (string, error) {
	var dir string
	switch {
	case uid == 0:
		dir = filepath.Join("/var/lib", "satellite", "oci")
	case filepath.IsAbs(xdgDataHome):
		dir = filepath.Join(xdgDataHome, "satellite", "oci")
	default:
		home, err := homeDir()
		if err != nil {
			return "", fmt.Errorf("get home directory: %w", err)
		}
		dir = filepath.Join(home, ".local", "share", "satellite", "oci")
	}

	return filepath.Clean(dir), nil
}

// ResolvePathConfig validates and resolves all storage paths.
// It expands ~ in configDir, creates the directory structure,
// and returns absolute paths for all configuration files.
func ResolvePathConfig(configDir string) (*PathConfig, error) {
	return ResolvePathConfigWithStore(configDir, "")
}

// ResolvePathConfigWithStore is ResolvePathConfig with an explicit OCI store
// directory. A non-empty storeDir bypasses default-store selection, so it works
// without a home directory.
func ResolvePathConfigWithStore(configDir, storeDir string) (*PathConfig, error) {
	expanded, err := expandPath(configDir)
	if err != nil {
		return nil, fmt.Errorf("expand config directory path: %w", err)
	}

	expanded, err = filepath.Abs(expanded)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}

	if err := ensureDir(expanded); err != nil {
		return nil, err
	}

	if storeDir == "" {
		storeDir, err = defaultStoreDir(os.Geteuid(), os.Getenv("XDG_DATA_HOME"), os.UserHomeDir)
		if err != nil {
			return nil, err
		}
	}

	return &PathConfig{
		ConfigDir:        expanded,
		ConfigFile:       filepath.Join(expanded, "config.json"),
		PrevConfigFile:   filepath.Join(expanded, "prev_config.json"),
		StoreDir:         storeDir,
		StateFile:        filepath.Join(expanded, "state.json"),
		DesiredStateFile: filepath.Join(expanded, DefaultDesiredStateFile),
	}, nil
}
