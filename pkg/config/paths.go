package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathConfig holds all resolved file paths for satellite storage.
type PathConfig struct {
	ConfigDir      string
	ConfigFile     string
	PrevConfigFile string
	ZotTempConfig  string
	ZotStorageDir  string
	StateFile      string
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
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}

	// Verify writability
	testFile := filepath.Join(path, ".write-test")
	if err := os.WriteFile(testFile, []byte{}, 0600); err != nil {
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

// geteuid is a seam so tests can exercise the root branch without running as root.
//
//nolint:gochecknoglobals // function-typed test seam, not mutable state
var geteuid = os.Geteuid

// rootRegistryDataDir is the default registry storage directory when running
// as root or a system service.
const rootRegistryDataDir = "/var/lib/satellite/registry"

// DefaultRegistryDataDir returns the default registry storage directory:
//   - /var/lib/satellite/registry when running as root (euid 0)
//   - $XDG_DATA_HOME/satellite/registry when XDG_DATA_HOME is set
//   - ~/.local/share/satellite/registry otherwise
func DefaultRegistryDataDir() (string, error) {
	if geteuid() == 0 {
		return rootRegistryDataDir, nil
	}

	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "satellite", "registry"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	return filepath.Join(home, ".local", "share", "satellite", "registry"), nil
}

// ResolveRegistryDataDir returns the absolute, ensured-writable registry data
// directory. If override is non-empty it wins; otherwise DefaultRegistryDataDir
// is used. The chosen path is tilde-expanded, made absolute, and created if
// missing.
func ResolveRegistryDataDir(override string) (string, error) {
	dir := override
	if dir == "" {
		def, err := DefaultRegistryDataDir()
		if err != nil {
			return "", err
		}
		dir = def
	}

	expanded, err := expandPath(dir)
	if err != nil {
		return "", fmt.Errorf("expand registry data directory path: %w", err)
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve absolute registry data directory path: %w", err)
	}

	if err := ensureDir(abs); err != nil {
		return "", err
	}

	return abs, nil
}

// ResolvePathConfig validates and resolves all storage paths.
// It expands ~ in configDir, creates the directory structure,
// and returns absolute paths for all configuration files.
func ResolvePathConfig(configDir string) (*PathConfig, error) {
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

	return &PathConfig{
		ConfigDir:      expanded,
		ConfigFile:     filepath.Join(expanded, "config.json"),
		PrevConfigFile: filepath.Join(expanded, "prev_config.json"),
		ZotTempConfig:  filepath.Join(expanded, "zot-hot.json"),
		StateFile:      filepath.Join(expanded, "state.json"),
	}, nil
}

// BuildZotConfigWithStoragePath updates the Zot configuration JSON to use
// the specified storage directory path.
func BuildZotConfigWithStoragePath(storageDir string) (string, error) {
	var zotConfig map[string]any
	if err := json.Unmarshal([]byte(DefaultZotConfigJSON), &zotConfig); err != nil {
		return "", fmt.Errorf("unmarshal default Zot config: %w", err)
	}

	storage, ok := zotConfig["storage"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid Zot config: storage section not found")
	}

	storage["rootDirectory"] = storageDir

	updatedJSON, err := json.MarshalIndent(zotConfig, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal updated Zot config: %w", err)
	}

	return string(updatedJSON), nil
}
