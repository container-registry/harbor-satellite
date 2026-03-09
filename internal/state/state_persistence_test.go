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
