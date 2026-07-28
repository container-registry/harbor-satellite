package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PersistedGroupState is the serializable form of a group's replicated entities.
type PersistedGroupState struct {
	URL      string   `json:"url"`
	Entities []Entity `json:"entities"`
}

// PersistedState is the top-level struct written to state.json.
type PersistedState struct {
	ConfigDigest string                `json:"config_digest,omitempty"`
	Groups       []PersistedGroupState `json:"groups"`
}

// SaveState writes the current stateMap and configDigest to disk.
func SaveState(path string, stateMap []StateMap, configDigest string) error {
	persisted := PersistedState{
		ConfigDigest: configDigest,
		Groups:       make([]PersistedGroupState, 0, len(stateMap)),
	}
	for _, sm := range stateMap {
		persisted.Groups = append(persisted.Groups, PersistedGroupState{
			URL:      sm.url,
			Entities: sm.Entities,
		})
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		if closeErr := tmp.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close temp file: %w", closeErr)
		}
		if err != nil {
			if removeErr := os.Remove(tmpName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = fmt.Errorf("%w (cleanup failed: %w)", err, removeErr)
			}
		}
	}
	defer cleanup()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp to state file: %w", err)
	}

	return nil
}

// LoadState reads the persisted state from disk.
// Returns nil, nil if the file does not exist.
func LoadState(path string) (*PersistedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var persisted PersistedState
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("unmarshal state file: %w", err)
	}

	return &persisted, nil
}

// ValidateStateFile reads and validates the state file for headless mode.
func ValidateStateFile(path string) error {
	persisted, err := LoadState(path)
	if err != nil {
		return fmt.Errorf("invalid or corrupted state file: %w", err)
	}
	if persisted == nil {
		return fmt.Errorf("state file not found at %s", path)
	}
	for i, g := range persisted.Groups {
		if g.URL == "" {
			return fmt.Errorf("group %d has empty URL", i)
		}
		for j, e := range g.Entities {
			if e.Name == "" {
				return fmt.Errorf("entity %d in group %s has empty name", j, g.URL)
			}
			if e.Tag == "" {
				return fmt.Errorf("entity %d in group %s has empty tag", j, g.URL)
			}
			if e.Digest == "" {
				return fmt.Errorf("entity %d in group %s has empty digest", j, g.URL)
			}
		}
	}
	return nil
}

