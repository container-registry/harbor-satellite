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
		mirrorList := strings.Split(parts[1], ",")

		switch cri {
		case "docker":
			if err := setDockerdConfig(mirrorList, localRegistry); err != nil {
				return fmt.Errorf("%s config error: %w", cri, err)
			}
		// crio and podman both use the same config
		case "crio", "podman":
			if err := setCrioConfig(mirrorList, localRegistry); err != nil {
				return fmt.Errorf("%s config error: %w", cri, err)
			}
		case "containerd":
			if err := setContainerdConfig(mirrorList, localRegistry); err != nil {
				return fmt.Errorf("%s config error: %w", cri, err)
			}
		default:
			return fmt.Errorf("unsupported CRI: %s", cri)
		}
	}
	return nil
}
