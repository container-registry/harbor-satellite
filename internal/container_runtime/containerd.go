package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	containerdCertsDir   = "/etc/containerd/certs.d"
	containerdConfigPath = "/etc/containerd/config.toml"
)

// setContainerdConfig writes hosts.toml for multiple upstream registries and updates containerd registry plugin
func setContainerdConfig(upstreamRegistries []string, localMirror string) error {
	// Update containerd main config
	if err := configureContainerd(containerdCertsDir); err != nil {
		return fmt.Errorf("failed to configure registry plugin: %w", err)
	}

	// Write hosts.toml for each registry
	for _, registryURL := range upstreamRegistries {
		if err := writeContainerdHostToml(registryURL, localMirror); err != nil {
			return fmt.Errorf("failed to configure containerd for %s: %w", registryURL, err)
		}
	}

	return nil
}

// writeContainerdHostToml creates or updates hosts.toml for a registry
func writeContainerdHostToml(registryURL, localMirror string) error {
	dir := filepath.Join(containerdCertsDir, registryURL)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if !strings.HasPrefix(registryURL, "http://") && !strings.HasPrefix(registryURL, "https://") {
		registryURL = "https://" + registryURL
	}

	path := filepath.Join(dir, "hosts.toml")
	var cfg ContainerdHosts

	// Load existing hosts.toml if exists
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}
	}

	if cfg.Host == nil {
		cfg.Host = make(map[string]Host)
	}
	cfg.Server = registryURL

	if !strings.HasPrefix(localMirror, "http://") && !strings.HasPrefix(localMirror, "https://") {
		localMirror = "http://" + localMirror
	}

	cfg.Host[localMirror] = Host{
		Capabilities: []string{"pull", "resolve"},
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", path, err)
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(cfg)
}

// configureContainerd updates only the registry config path in containerd main config
func configureContainerd(certDir string) error {
	var cfg map[string]interface{}

	// Load entire existing config into map
	if _, err := os.Stat(containerdConfigPath); err == nil {
		if _, err := toml.DecodeFile(containerdConfigPath, &cfg); err != nil {
			return fmt.Errorf("failed to parse %s: %w", containerdConfigPath, err)
		}
	} else {
		cfg = make(map[string]interface{})
	}

	// create/initialise nested maps required
	plugins, ok := cfg["plugins"].(map[string]interface{})
	if !ok || plugins == nil {
		plugins = make(map[string]interface{})
		cfg["plugins"] = plugins
	}

	criImages, ok := plugins["io.containerd.cri.v1.images"].(map[string]interface{})
	if !ok || criImages == nil {
		criImages = make(map[string]interface{})
		plugins["io.containerd.cri.v1.images"] = criImages
	}

	registryMap, ok := criImages["registry"].(map[string]interface{})
	if !ok || registryMap == nil {
		registryMap = make(map[string]interface{})
		criImages["registry"] = registryMap
	}

	registryMap["config_path"] = certDir

	f, err := os.Create(containerdConfigPath)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", containerdConfigPath, err)
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(cfg)
}
