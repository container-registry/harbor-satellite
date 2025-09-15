package runtime

import (
	"fmt"
	"strings"
)

// ApplyCRIConfigs applies mirror configs to the appropriate runtime.
func ApplyCRIConfigs(mirrorsMap []string, localRegistry string) error {

	for _, entry := range mirrorsMap {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid mirror format: %s", entry)
		}

		cri := parts[0]
		mirror := parts[1]

		switch cri {
		case "docker":
			if err := setDockerdConfig(mirror, localRegistry); err != nil {
				return fmt.Errorf("docker config error: %w", err)
			}
		case "crio", "podman":
			// TODO: implement CRI-O/Podman logic
		case "containerd":
			// TODO: implement containerd logic
		default:
			return fmt.Errorf("unsupported CRI: %s", cri)
		}
	}

	return nil
}
