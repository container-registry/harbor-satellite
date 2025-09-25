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
	if err := configureContainerd(containerdCertsDir); err != nil {
		return fmt.Errorf("failed to configure registry plugin: %w", err)
	}

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
	defer func() {
		_ = f.Close()
	}()

	return toml.NewEncoder(f).Encode(cfg)
}

// configureContainerd updates only the registry config path in containerd main config
func configureContainerd(certDir string) error {
	cfg, err := loadToml(containerdConfigPath)
	if err != nil {
		return err
	}

	// do not overwrite existing config
	plugins := loadNestedMap(cfg, "plugins")
	criImages := loadNestedMap(plugins, "io.containerd.cri.v1.images")
	registryMap := loadNestedMap(criImages, "registry")

	registryMap["config_path"] = certDir

	f, err := os.Create(containerdConfigPath)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", containerdConfigPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	return toml.NewEncoder(f).Encode(cfg)
}

// loadToml loads existing TOML into a flexible type
func loadToml(path string) (map[string]interface{}, error) {
	cfg := make(map[string]interface{})
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
	}
	return cfg, nil
}

func loadNestedMap(parent map[string]interface{}, key string) map[string]interface{} {
	if v, ok := parent[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	newMap := make(map[string]interface{})
	parent[key] = newMap
	return newMap
}
