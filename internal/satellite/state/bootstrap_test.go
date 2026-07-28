package state

import (
	"archive/tar"
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/stretchr/testify/require"
)

func TestExtractTar(t *testing.T) {
	tempDir := t.TempDir()
	tarFile := filepath.Join(tempDir, "test.tar")

	// Create a dummy tarball
	f, err := os.Create(tarFile)
	require.NoError(t, err)
	defer f.Close()

	tw := tar.NewWriter(f)
	
	// Add a directory
	err = tw.WriteHeader(&tar.Header{
		Name:     "subfolder/",
		Typeflag: tar.TypeDir,
	})
	require.NoError(t, err)

	// Add a file
	content := []byte("hello world")
	err = tw.WriteHeader(&tar.Header{
		Name:     "subfolder/file.txt",
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
		Mode:     0o644,
	})
	require.NoError(t, err)
	_, err = tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	// Extract
	extractDir := filepath.Join(tempDir, "extract")
	require.NoError(t, extractTar(tarFile, extractDir))

	// Verify
	extractedFile := filepath.Join(extractDir, "subfolder/file.txt")
	require.FileExists(t, extractedFile)
	data, err := os.ReadFile(extractedFile)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(data))
}

func TestBootstrapSatellite(t *testing.T) {
	// Start in-memory mock registry
	regHandler := registry.New()
	server := httptest.NewServer(regHandler)
	defer server.Close()

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	tempDir := t.TempDir()
	ociPath := filepath.Join(tempDir, "oci")

	// Programmatically write OCI layout
	path, err := layout.Write(ociPath, empty.Index)
	require.NoError(t, err)

	img := empty.Image
	err = path.AppendImage(img, layout.WithAnnotations(map[string]string{
		"org.opencontainers.image.ref.name": "library/alpine:latest",
	}))
	require.NoError(t, err)

	stateFile := filepath.Join(tempDir, "state.json")

	opts := BootstrapOptions{
		ArtifactPath:  ociPath,
		ConfigDir:     tempDir,
		StateFilePath: stateFile,
		RegistryURL:   u.Host,
		UseUnsecure:   true,
	}

	err = BootstrapSatellite(context.Background(), opts)
	require.NoError(t, err)

	// Verify state file generated
	require.FileExists(t, stateFile)
	persisted, err := LoadState(stateFile)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	require.Len(t, persisted.Groups, 1)
	require.Equal(t, "offline-bootstrap", persisted.Groups[0].URL)
	require.Len(t, persisted.Groups[0].Entities, 1)
	require.Equal(t, "alpine", persisted.Groups[0].Entities[0].Name)
	require.Equal(t, "library", persisted.Groups[0].Entities[0].Repository)
	require.Equal(t, "latest", persisted.Groups[0].Entities[0].Tag)
}
