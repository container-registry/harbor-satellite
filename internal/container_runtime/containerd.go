package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const containerdCertsDir = "/etc/containerd/certs.d"

// setContainerdConfig writes hosts.toml for multiple upstream registries with a single local mirror
func setContainerdConfig(upstreamRegistries []string, localMirror string) error {

	for _, registryURL := range upstreamRegistries {
		if err := writeContainerdHostToml(registryURL, localMirror); err != nil {
			return fmt.Errorf("failed to configure containerd for %s: %w", registryURL, err)
		}
	}
	return nil
}

func writeContainerdHostToml(registryURL, localMirror string) error {

	dir := filepath.Join(containerdCertsDir, registryURL)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// use https by default
	if !strings.HasPrefix(registryURL, "http://") && !strings.HasPrefix(registryURL, "https://") {
		registryURL = "https://" + registryURL
	}

	path := filepath.Join(dir, "hosts.toml")
	var cfg ContainerdHosts

	// Load existing file if present
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}
	} else {
		cfg.Host = make(map[string]Host)
	}

	cfg.Server = registryURL

	// Ensure local mirror has http/https scheme
	mirrorKey := localMirror
	if !strings.HasPrefix(localMirror, "http://") && !strings.HasPrefix(localMirror, "https://") {
		mirrorKey = "http://" + localMirror
	}

	// Add local mirror under the host section
	cfg.Host[mirrorKey] = Host{
		Capabilities: []string{"pull", "resolve"},
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode TOML: %w", err)
	}

	return nil
}
