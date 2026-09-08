package state

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/require"
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

func TestDeliver_DigestPinned(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgA, err := random.Image(1024, 1)
	require.NoError(t, err)
	refA, err := name.ParseReference(host+"/repo/name:v1", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(refA, imgA))
	digestA, err := imgA.Digest()
	require.NoError(t, err)

	imgB, err := random.Image(1024, 1)
	require.NoError(t, err)
	refB, err := name.ParseReference(host+"/repo/name:v1", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(refB, imgB))

	dir := t.TempDir()
	d := NewDirectDeliverer(dir, "", "", host, true)

	entity := Entity{Repository: "repo", Name: "name", Tag: "v1", Digest: digestA.String()}
	require.NoError(t, d.Deliver(testContext(), []Entity{entity}))

	tarPath := filepath.Join(dir, tarballFilename(entity))
	got, err := tarball.ImageFromPath(tarPath, nil)
	require.NoError(t, err)
	gotDigest, err := got.Digest()
	require.NoError(t, err)
	require.Equal(t, digestA, gotDigest, "should deliver the pinned digest, not the moved tag")

	// Verify the tarball's RepoTags uses the tag, not the digest ref,
	// so k3s imports the image under the expected name:tag.
	manifest, err := tarball.LoadManifest(func() (io.ReadCloser, error) { return os.Open(tarPath) })
	require.NoError(t, err)
	require.NotEmpty(t, manifest)
	require.NotEmpty(t, manifest[0].RepoTags, "tarball must carry RepoTags for k3s import")
	require.Contains(t, manifest[0].RepoTags[0], ":v1", "RepoTags should use tag, not digest ref")
	require.NotContains(t, manifest[0].RepoTags[0], "@sha256:", "RepoTags must not contain digest ref")
}

func TestDeliver_TagFallback(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgB, err := random.Image(1024, 1)
	require.NoError(t, err)
	ref, err := name.ParseReference(host+"/repo/name:v1", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(ref, imgB))
	digestB, err := imgB.Digest()
	require.NoError(t, err)

	dir := t.TempDir()
	d := NewDirectDeliverer(dir, "", "", host, true)

	entity := Entity{Repository: "repo", Name: "name", Tag: "v1", Digest: ""}
	require.NoError(t, d.Deliver(testContext(), []Entity{entity}))

	got, err := tarball.ImageFromPath(filepath.Join(dir, tarballFilename(entity)), nil)
	require.NoError(t, err)
	gotDigest, err := got.Digest()
	require.NoError(t, err)
	require.Equal(t, digestB, gotDigest, "should deliver the tag-resolved image when no digest is present")
}
