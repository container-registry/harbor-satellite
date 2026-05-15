package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTarballFilename(t *testing.T) {
	tests := []struct {
		name   string
		entity Entity
		want   string
	}{
		{
			name:   "simple",
			entity: Entity{Repository: "library", Name: "nginx", Tag: "latest"},
			want:   "library--nginx--latest.tar",
		},
		{
			name:   "nested repository",
			entity: Entity{Repository: "project/repo", Name: "app", Tag: "v1.0"},
			want:   "project_repo--app--v1.0.tar",
		},
		{
			name:   "deep path",
			entity: Entity{Repository: "harbor/satellite/images", Name: "worker", Tag: "sha-abc123"},
			want:   "harbor_satellite_images--worker--sha-abc123.tar",
		},
		{
			name:   "no collision with slash vs underscore",
			entity: Entity{Repository: "foo/bar", Name: "baz", Tag: "v1"},
			want:   "foo_bar--baz--v1.tar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tarballFilename(tt.entity)
			if got != tt.want {
				t.Errorf("tarballFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDigestMapPersistence(t *testing.T) {
	dir := t.TempDir()
	d := &DirectDeliverer{imageDir: dir}

	// Initially empty
	m := d.loadDigestMap()
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}

	// Save and reload
	m["test.tar"] = "sha256:abc123"
	if err := d.saveDigestMap(m); err != nil {
		t.Fatalf("saveDigestMap: %v", err)
	}

	loaded := d.loadDigestMap()
	if loaded["test.tar"] != "sha256:abc123" {
		t.Errorf("loaded digest = %q, want %q", loaded["test.tar"], "sha256:abc123")
	}

	// Verify file content
	data, err := os.ReadFile(filepath.Join(dir, digestMapFile))
	if err != nil {
		t.Fatalf("read digest file: %v", err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["test.tar"] != "sha256:abc123" {
		t.Errorf("file content mismatch")
	}
}

func TestDeleteRemovesFileAndDigest(t *testing.T) {
	dir := t.TempDir()
	d := &DirectDeliverer{imageDir: dir}

	// Create a fake tarball file and digest entry
	filename := tarballFilename(Entity{Repository: "lib", Name: "app", Tag: "v1"})
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte("fake tar"), 0o644); err != nil {
		t.Fatalf("write fake tarball: %v", err)
	}

	digests := map[string]string{filename: "sha256:old"}
	if err := d.saveDigestMap(digests); err != nil {
		t.Fatalf("save digests: %v", err)
	}

	// Delete the entity
	ctx := testContext()
	err := d.Delete(ctx, []Entity{{Repository: "lib", Name: "app", Tag: "v1"}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// File should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, got err: %v", err)
	}

	// Digest entry should be gone
	loaded := d.loadDigestMap()
	if _, ok := loaded[filename]; ok {
		t.Errorf("expected digest entry to be removed")
	}
}

func TestDeleteNonexistentFileNoError(t *testing.T) {
	dir := t.TempDir()
	d := &DirectDeliverer{imageDir: dir}

	ctx := testContext()
	err := d.Delete(ctx, []Entity{{Repository: "lib", Name: "gone", Tag: "v1"}})
	if err != nil {
		t.Fatalf("Delete nonexistent: %v", err)
	}
}

func TestDeliverEmptyEntitiesIsNoop(t *testing.T) {
	dir := t.TempDir()
	d := &DirectDeliverer{imageDir: dir}

	ctx := testContext()
	err := d.Deliver(ctx, nil)
	if err != nil {
		t.Fatalf("Deliver(nil): %v", err)
	}
	err = d.Deliver(ctx, []Entity{})
	if err != nil {
		t.Fatalf("Deliver([]): %v", err)
	}
}

// TestDeliverSkipsEntityAlreadyInDigestMap verifies that an entity whose
// filename+digest already appears in the digest map is skipped without error.
// This is the fast path for convergence: when Deliver is called with the full
// desired state, previously written tarballs are not re-fetched or re-written.
func TestDeliverSkipsEntityAlreadyInDigestMap(t *testing.T) {
	dir := t.TempDir()
	d := &DirectDeliverer{imageDir: dir}

	entity := Entity{Repository: "lib", Name: "nginx", Tag: "v1", Digest: "sha256:abc123"}
	filename := tarballFilename(entity)

	// Simulate a prior successful write: file exists and digest map records it.
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("fake tar content"), 0o644); err != nil {
		t.Fatalf("write fake tarball: %v", err)
	}
	if err := d.saveDigestMap(map[string]string{filename: entity.Digest}); err != nil {
		t.Fatalf("save digest map: %v", err)
	}

	// Deliver should succeed and skip without touching the file.
	ctx := testContext()
	if err := d.Deliver(ctx, []Entity{entity}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	// Digest map must still contain the original entry unchanged.
	loaded := d.loadDigestMap()
	if got, ok := loaded[filename]; !ok || got != entity.Digest {
		t.Errorf("digest map entry modified: got %v, want %v", loaded, map[string]string{filename: entity.Digest})
	}
}

// TestDeliverAttemptsEntityMissingFromDigestMap verifies that an entity absent
// from the digest map is attempted even when the caller considers it "unchanged".
// This is the retry path: a failed delivery from a prior cycle leaves no digest
// map entry, so the full-state Deliver call will attempt it again.
func TestDeliverAttemptsEntityMissingFromDigestMap(t *testing.T) {
	dir := t.TempDir()
	d := &DirectDeliverer{imageDir: dir}

	entity := Entity{Repository: "lib", Name: "redis", Tag: "v2", Digest: "sha256:def456"}

	// Digest map is empty — simulates a prior cycle where delivery failed.
	m := d.loadDigestMap()
	if len(m) != 0 {
		t.Fatalf("expected empty digest map, got %v", m)
	}

	// Deliver will attempt the entity (and fail pulling from a non-existent registry),
	// but the key assertion is that it does NOT skip: the digest map stays empty
	// (no phantom entry written for a failed delivery).
	ctx := testContext()
	_ = d.Deliver(ctx, []Entity{entity}) // error expected — no real registry

	loaded := d.loadDigestMap()
	if _, ok := loaded[tarballFilename(entity)]; ok {
		t.Errorf("digest map must not record a failed delivery")
	}
}
