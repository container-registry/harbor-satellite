package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	stateMap := []StateMap{
		{
			url: "http://registry.example.com/group1",
			Entities: []Entity{
				{Name: "alpine", Repository: "library", Tag: "latest", Digest: "sha256:abc123"},
				{Name: "nginx", Repository: "library", Tag: "1.25", Digest: "sha256:def456"},
			},
		},
		{
			url: "http://registry.example.com/group2",
			Entities: []Entity{
				{Name: "redis", Repository: "library", Tag: "7", Digest: "sha256:ghi789"},
			},
		},
	}
	configDigest := "sha256:config123"

	if err := SaveState(path, stateMap, configDigest); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil")
	}

	if loaded.ConfigDigest != configDigest {
		t.Errorf("ConfigDigest = %q, want %q", loaded.ConfigDigest, configDigest)
	}

	if len(loaded.Groups) != len(stateMap) {
		t.Fatalf("Groups count = %d, want %d", len(loaded.Groups), len(stateMap))
	}

	for i, g := range loaded.Groups {
		if g.URL != stateMap[i].url {
			t.Errorf("Group[%d].URL = %q, want %q", i, g.URL, stateMap[i].url)
		}
		if len(g.Entities) != len(stateMap[i].Entities) {
			t.Fatalf("Group[%d].Entities count = %d, want %d", i, len(g.Entities), len(stateMap[i].Entities))
		}
		for j, e := range g.Entities {
			want := stateMap[i].Entities[j]
			if e.Name != want.Name || e.Repository != want.Repository || e.Tag != want.Tag || e.Digest != want.Digest {
				t.Errorf("Group[%d].Entities[%d] = %+v, want %+v", i, j, e, want)
			}
		}
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState returned error for missing file: %v", err)
	}
	if loaded != nil {
		t.Fatalf("LoadState returned non-nil for missing file: %+v", loaded)
	}
}

func TestSaveEmptyState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := SaveState(path, nil, ""); err != nil {
		t.Fatalf("SaveState failed for empty state: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil for empty state")
	}
	if loaded.ConfigDigest != "" {
		t.Errorf("ConfigDigest = %q, want empty", loaded.ConfigDigest)
	}
	if len(loaded.Groups) != 0 {
		t.Errorf("Groups count = %d, want 0", len(loaded.Groups))
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist: %v", err)
	}
}

func TestSaveState_InvalidPath(t *testing.T) {
	// Try to save to a directory that doesn't exist and can't be created
	path := "/nonexistent/deeply/nested/path/state.json"

	stateMap := []StateMap{
		{
			url: "http://registry.example.com/group1",
			Entities: []Entity{
				{Name: "alpine", Repository: "library", Tag: "latest", Digest: "sha256:abc123"},
			},
		},
	}

	err := SaveState(path, stateMap, "sha256:config123")
	if err == nil {
		t.Fatal("SaveState should fail with invalid path")
	}
}

func TestSaveState_ReadOnlyDirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping read-only test when running as root")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Make directory read-only
	if err := os.Chmod(dir, 0444); err != nil {
		t.Fatalf("Failed to make directory read-only: %v", err)
	}
	defer os.Chmod(dir, 0755) // Restore permissions for cleanup

	stateMap := []StateMap{
		{
			url: "http://registry.example.com/group1",
			Entities: []Entity{
				{Name: "alpine", Repository: "library", Tag: "latest", Digest: "sha256:abc123"},
			},
		},
	}

	err := SaveState(path, stateMap, "sha256:config123")
	if err == nil {
		t.Fatal("SaveState should fail with read-only directory")
	}
}

func TestLoadState_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")

	// Write invalid JSON
	if err := os.WriteFile(path, []byte("not valid json"), 0600); err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}

	loaded, err := LoadState(path)
	if err == nil {
		t.Fatal("LoadState should fail with invalid JSON")
	}
	if loaded != nil {
		t.Errorf("LoadState returned non-nil with invalid JSON: %+v", loaded)
	}
}

func TestLoadState_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.json")

	// Write corrupted JSON (valid JSON but unexpected structure)
	if err := os.WriteFile(path, []byte(`{"unexpected": "structure"}`), 0600); err != nil {
		t.Fatalf("Failed to write corrupted JSON: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed with corrupted JSON: %v", err)
	}
	// Should succeed but return default values
	if loaded == nil {
		t.Fatal("LoadState returned nil with corrupted JSON")
	}
}

func TestSaveState_LargeState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large-state.json")

	// Create a large state with many entities
	var stateMap []StateMap
	for i := 0; i < 100; i++ {
		entities := make([]Entity, 50)
		for j := 0; j < 50; j++ {
			entities[j] = Entity{
				Name:       "image" + string(rune(j)),
				Repository: "repo" + string(rune(i)),
				Tag:        "v1.0.0",
				Digest:     "sha256:abc123def456",
			}
		}
		stateMap = append(stateMap, StateMap{
			url:      "http://registry.example.com/group" + string(rune(i)),
			Entities: entities,
		})
	}

	err := SaveState(path, stateMap, "sha256:large-config")
	if err != nil {
		t.Fatalf("SaveState failed with large state: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed with large state: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil for large state")
	}
	if len(loaded.Groups) != 100 {
		t.Errorf("Groups count = %d, want 100", len(loaded.Groups))
	}
}

func TestSaveState_SpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "special-chars.json")

	stateMap := []StateMap{
		{
			url: "http://registry.example.com/group-with-特殊字符",
			Entities: []Entity{
				{
					Name:       "image-with-émojis-🚀",
					Repository: "repo/with/slashes",
					Tag:        "v1.0.0-特殊",
					Digest:     "sha256:abc123",
				},
			},
		},
	}

	err := SaveState(path, stateMap, "sha256:config-with-特殊")
	if err != nil {
		t.Fatalf("SaveState failed with special characters: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil")
	}

	if loaded.ConfigDigest != "sha256:config-with-特殊" {
		t.Errorf("ConfigDigest = %q, want %q", loaded.ConfigDigest, "sha256:config-with-特殊")
	}

	if len(loaded.Groups) != 1 {
		t.Fatalf("Groups count = %d, want 1", len(loaded.Groups))
	}

	if loaded.Groups[0].Entities[0].Name != "image-with-émojis-🚀" {
		t.Errorf("Entity name = %q, want %q", loaded.Groups[0].Entities[0].Name, "image-with-émojis-🚀")
	}
}

func TestSaveState_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.json")

	// Test that concurrent saves don't corrupt the file
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func(id int) {
			stateMap := []StateMap{
				{
					url: "http://registry.example.com/group" + string(rune(id)),
					Entities: []Entity{
						{Name: "image", Repository: "repo", Tag: "v1", Digest: "sha256:abc"},
					},
				},
			}
			err := SaveState(path, stateMap, "sha256:config"+string(rune(id)))
			if err != nil {
				t.Logf("SaveState failed (expected with concurrent writes): %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify the file is valid JSON (one of the writes should have succeeded)
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed after concurrent writes: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil after concurrent writes")
	}
}

func TestSaveState_EmptyGroups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-groups.json")

	stateMap := []StateMap{
		{
			url:      "http://registry.example.com/empty-group",
			Entities: []Entity{},
		},
	}

	err := SaveState(path, stateMap, "sha256:config")
	if err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil")
	}

	if len(loaded.Groups) != 1 {
		t.Fatalf("Groups count = %d, want 1", len(loaded.Groups))
	}

	if len(loaded.Groups[0].Entities) != 0 {
		t.Errorf("Entities count = %d, want 0", len(loaded.Groups[0].Entities))
	}
}

func TestLoadState_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")

	// Write empty file
	if err := os.WriteFile(path, []byte(""), 0600); err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	loaded, err := LoadState(path)
	if err == nil {
		t.Fatal("LoadState should fail with empty file")
	}
	if loaded != nil {
		t.Errorf("LoadState returned non-nil with empty file: %+v", loaded)
	}
}

func TestSaveState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.json")

	// First write
	stateMap1 := []StateMap{
		{
			url: "http://registry.example.com/group1",
			Entities: []Entity{
				{Name: "alpine", Repository: "library", Tag: "latest", Digest: "sha256:abc123"},
			},
		},
	}
	err := SaveState(path, stateMap1, "sha256:config1")
	if err != nil {
		t.Fatalf("First SaveState failed: %v", err)
	}

	// Second write should replace first atomically
	stateMap2 := []StateMap{
		{
			url: "http://registry.example.com/group2",
			Entities: []Entity{
				{Name: "nginx", Repository: "library", Tag: "latest", Digest: "sha256:def456"},
			},
		},
	}
	err = SaveState(path, stateMap2, "sha256:config2")
	if err != nil {
		t.Fatalf("Second SaveState failed: %v", err)
	}

	// Load should get the second state
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil")
	}

	if loaded.ConfigDigest != "sha256:config2" {
		t.Errorf("ConfigDigest = %q, want sha256:config2", loaded.ConfigDigest)
	}

	if len(loaded.Groups) != 1 {
		t.Fatalf("Groups count = %d, want 1", len(loaded.Groups))
	}

	if loaded.Groups[0].Entities[0].Name != "nginx" {
		t.Errorf("Entity name = %q, want nginx", loaded.Groups[0].Entities[0].Name)
	}
}