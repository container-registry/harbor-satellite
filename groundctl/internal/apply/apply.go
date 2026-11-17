package apply

import (
	"context"
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/groundctl/internal/client"
	"gopkg.in/yaml.v3"
)

// FleetManifest is the schema for groundctl apply -f fleet.yaml.
type FleetManifest struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   ManifestMeta   `yaml:"metadata"`
	Spec       FleetSpec      `yaml:"spec"`
}

// ManifestMeta holds the name of the fleet manifest.
type ManifestMeta struct {
	Name string `yaml:"name"`
}

// FleetSpec describes the desired state of the satellite fleet.
type FleetSpec struct {
	// ConfigName is the default config applied to all satellites unless overridden.
	ConfigName string             `yaml:"configName"`
	// Groups is the default set of image groups for all satellites unless overridden.
	Groups     []string           `yaml:"groups,omitempty"`
	// Satellites is the list of desired satellites.
	Satellites []SatelliteEntry   `yaml:"satellites"`
}

// SatelliteEntry describes one desired satellite in the fleet.
type SatelliteEntry struct {
	Name       string   `yaml:"name"`
	ConfigName string   `yaml:"configName,omitempty"` // overrides fleet-level config
	Groups     []string `yaml:"groups,omitempty"`     // overrides fleet-level groups
}

// ApplyResult holds the diff produced by an apply operation.
type ApplyResult struct {
	Created   []string
	Deleted   []string
	Updated   []string
	Unchanged []string
}

// ParseManifest reads and parses a fleet YAML file from disk.
func ParseManifest(path string) (*FleetManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %q: %w", path, err)
	}

	var manifest FleetManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest %q: %w", path, err)
	}

	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// validateManifest checks required fields in the manifest.
func validateManifest(m *FleetManifest) error {
	if m.Kind != "SatelliteFleet" {
		return fmt.Errorf("unsupported manifest kind %q: expected SatelliteFleet", m.Kind)
	}
	if m.Spec.ConfigName == "" {
		return fmt.Errorf("spec.configName is required")
	}
	if len(m.Spec.Satellites) == 0 {
		return fmt.Errorf("spec.satellites must have at least one entry")
	}
	seen := map[string]bool{}
	for _, s := range m.Spec.Satellites {
		if s.Name == "" {
			return fmt.Errorf("each satellite entry must have a name")
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate satellite name %q in manifest", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
}

// Reconcile diffs the desired fleet manifest against live Ground Control state
// and applies the minimal set of changes needed to converge.
func Reconcile(ctx context.Context, gc *client.Client, manifest *FleetManifest, dryRun bool) (*ApplyResult, error) {
	// Fetch current state
	current, err := gc.ListSatellites(ctx, client.ListSatellitesParams{})
	if err != nil {
		return nil, fmt.Errorf("fetch current satellites: %w", err)
	}

	// Build lookup maps
	currentMap := make(map[string]client.Satellite, len(current))
	for _, s := range current {
		currentMap[s.Name] = s
	}

	desiredMap := make(map[string]SatelliteEntry, len(manifest.Spec.Satellites))
	for _, s := range manifest.Spec.Satellites {
		desiredMap[s.Name] = s
	}

	result := &ApplyResult{}

	// Create satellites that exist in desired but not in current
	for name, entry := range desiredMap {
		if _, exists := currentMap[name]; !exists {
			configName := entry.ConfigName
			if configName == "" {
				configName = manifest.Spec.ConfigName
			}
			groups := entry.Groups
			if len(groups) == 0 {
				groups = manifest.Spec.Groups
			}

			if !dryRun {
				params := client.RegisterSatelliteParams{
					Name:       name,
					ConfigName: configName,
				}
				if len(groups) > 0 {
					params.Groups = &groups
				}
				if _, err := gc.RegisterSatellite(ctx, params); err != nil {
					return nil, fmt.Errorf("register satellite %q: %w", name, err)
				}
			}
			result.Created = append(result.Created, name)
		} else {
			// Already exists — mark unchanged (group-level reconciliation is a future enhancement)
			result.Unchanged = append(result.Unchanged, name)
		}
	}

	// Delete satellites that exist in current but not in desired
	for name := range currentMap {
		if _, wanted := desiredMap[name]; !wanted {
			if !dryRun {
				if err := gc.DeleteSatellite(ctx, name); err != nil {
					return nil, fmt.Errorf("delete satellite %q: %w", name, err)
				}
			}
			result.Deleted = append(result.Deleted, name)
		}
	}

	return result, nil
}
