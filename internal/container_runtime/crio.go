package runtime

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
	"strings"
)

const registriesConfPath = "/etc/containers/registries.conf"

// setCrioConfig configures mirrors for crio and podman
func setCrioConfig(upstreamRegistries []string, localMirror string) error {
	data := map[string]interface{}{}
	if _, err := toml.DecodeFile(registriesConfPath, &data); err != nil {
		return fmt.Errorf("failed to parse %s: %w", registriesConfPath, err)
	}

	// esnsure registry section exists
	regs, ok := data["registry"].([]map[string]interface{})
	if !ok {
		regs = []map[string]interface{}{}
	}

	insecure := !strings.HasPrefix(localMirror, "https")

	for _, upstream := range upstreamRegistries {
		upstream = strings.TrimSpace(upstream)
		if upstream == "" {
			continue
		}

		var found bool
		for _, r := range regs {
			if r["location"] == upstream {
				// append mirror if missing
				mirrors, _ := r["mirror"].([]map[string]interface{})
				exists := false
				for _, m := range mirrors {
					if m["location"] == localMirror {
						exists = true
						break
					}
				}
				if !exists {
					mirrors = append(mirrors, map[string]interface{}{
						"location": localMirror,
						"insecure": insecure,
					})
				}
				r["mirror"] = mirrors
				found = true
				break
			}
		}

		if !found {
			regs = append(regs, map[string]interface{}{
				"location": upstream,
				"mirror": []map[string]interface{}{
					{
						"location": localMirror,
						"insecure": insecure,
					},
				},
			})
		}
	}

	data["registry"] = regs

	f, err := os.Create(registriesConfPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", registriesConfPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	return toml.NewEncoder(f).Encode(data)
}
