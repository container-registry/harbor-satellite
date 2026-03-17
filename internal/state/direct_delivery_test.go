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
			want:   "library_nginx_latest.tar",
		},
		{
			name:   "nested repository",
			entity: Entity{Repository: "project/repo", Name: "app", Tag: "v1.0"},
			want:   "project_repo_app_v1.0.tar",
		},
		{
			name:   "deep path",
			entity: Entity{Repository: "harbor/satellite/images", Name: "worker", Tag: "sha-abc123"},
			want:   "harbor_satellite_images_worker_sha-abc123.tar",
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
