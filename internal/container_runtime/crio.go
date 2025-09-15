package runtime

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
	"strings"
)

const registriesConfPath = "/etc/containers/registries.conf"

// SetLocalMirrorForRegistries adds a single local mirror to multiple upstream registries
func setCrioConfig(upstreamRegistries []string, localMirror string) error {
	var cfg RegistriesConf

	// Load existing config if exists
	if _, err := os.Stat(registriesConfPath); err == nil {
		if _, err := toml.DecodeFile(registriesConfPath, &cfg); err != nil {
			return fmt.Errorf("failed to parse %s: %w", registriesConfPath, err)
		}
	}

	// Determine insecure flag from localMirror
	insecure := !strings.HasPrefix(localMirror, "https")

	for _, upstream := range upstreamRegistries {
		upstream = strings.TrimSpace(upstream)
		if upstream == "" {
			continue
		}

		// Find existing registry or create new
		var reg *Registry
		for idx, r := range cfg.Registries {
			if r.Location == upstream {
				reg = &cfg.Registries[idx]
				break
			}
		}
		if reg == nil {
			cfg.Registries = append(cfg.Registries, Registry{Location: upstream})
			reg = &cfg.Registries[len(cfg.Registries)-1]
		}

		// Append the local mirror if not already present
		found := false
		for _, m := range reg.Mirrors {
			if m.Location == localMirror {
				found = true
				break
			}
		}
		if !found {
			reg.Mirrors = append(reg.Mirrors, Mirror{
				Location: localMirror,
				Insecure: insecure,
			})
		}
	}

	// Write back to the file
	f, err := os.Create(registriesConfPath)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", registriesConfPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode TOML and write to file: %w", err)
	}

	return nil
}
