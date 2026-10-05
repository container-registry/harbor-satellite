package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	gcremote "github.com/google/go-containerregistry/pkg/v1/remote"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

func TestNewOCIStoreRequiresRoot(t *testing.T) {
	storage, err := NewOCIStore("", RegistryOptions{Endpoint: "registry.example.com"})
	require.Error(t, err)
	require.Nil(t, storage)
}

func TestOCIStoreReplicatePersistsImageLayout(t *testing.T) {
	source := newTestRegistry(t)
	image := pushImage(t, source, "alpine", "latest", 2)
	root := t.TempDir()
	artifact := Artifact{Name: "alpine", Repository: "library", Tag: "latest"}

	storage, err := NewOCIStore(root, RegistryOptions{Endpoint: source, PlainHTTP: true})
	require.NoError(t, err)
	require.NoError(t, storage.Replicate(testContext(), []Artifact{artifact}))

	require.FileExists(t, filepath.Join(root, "oci-layout"))
	require.FileExists(t, filepath.Join(root, "index.json"))
	entries, err := os.ReadDir(filepath.Join(root, "blobs", "sha256"))
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	reopened, err := oci.New(root)
	require.NoError(t, err)
	desc, err := reopened.Resolve(testContext(), source+"/library/alpine:latest")
	require.NoError(t, err)
	wantDigest, err := image.Digest()
	require.NoError(t, err)
	require.Equal(t, wantDigest.String(), desc.Digest.String())
}

func TestOCIStoreReplicatesArbitraryArtifact(t *testing.T) {
	source := newTestRegistry(t)
	repository, err := orasremote.NewRepository(source + "/project/document")
	require.NoError(t, err)
	repository.PlainHTTP = true

	layer := []byte("satellite artifact payload")
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.document.layer.v1", layer)
	config := []byte(`{"kind":"document"}`)
	configDesc := content.NewDescriptorFromBytes("application/vnd.example.document.config.v1+json", config)
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: "application/vnd.example.document.v1",
		Config:       configDesc,
		Layers:       []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestBytes)

	ctx := testContext()
	require.NoError(t, repository.Push(ctx, layerDesc, bytes.NewReader(layer)))
	require.NoError(t, repository.Push(ctx, configDesc, bytes.NewReader(config)))
	require.NoError(t, repository.PushReference(ctx, manifestDesc, bytes.NewReader(manifestBytes), "v1"))

	storage, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: source, Repository: "project", PlainHTTP: true})
	require.NoError(t, err)
	require.NoError(t, storage.Replicate(ctx, []Artifact{{Name: "document", Tag: "v1"}}))

	stored, err := storage.target.Resolve(ctx, source+"/project/document:v1")
	require.NoError(t, err)
	require.Equal(t, manifestDesc.Digest, stored.Digest)
	storedLayer, err := content.FetchAll(ctx, storage.target, layerDesc)
	require.NoError(t, err)
	require.Equal(t, layer, storedLayer)
}

func TestOCIStoreDeleteRetainsSharedContent(t *testing.T) {
	source := newTestRegistry(t)
	image := pushImage(t, source, "first", "v1", 1)
	secondRef, err := name.ParseReference(source+"/library/second:v1", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, gcremote.Write(secondRef, image))

	ctx := testContext()
	storage, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: source, PlainHTTP: true})
	require.NoError(t, err)
	first := Artifact{Name: "first", Repository: "library", Tag: "v1"}
	second := Artifact{Name: "second", Repository: "library", Tag: "v1"}
	require.NoError(t, storage.Replicate(ctx, []Artifact{first, second}))
	require.NoError(t, storage.Delete(ctx, []Artifact{first}))

	_, err = storage.target.Resolve(ctx, source+"/library/first:v1")
	require.Error(t, err)
	require.True(t, errors.Is(err, errdef.ErrNotFound))
	_, err = storage.target.Resolve(ctx, source+"/library/second:v1")
	require.NoError(t, err)
}

func TestOCIStoreDeleteMissingReferenceIsIdempotent(t *testing.T) {
	storage, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: "registry.example.com"})
	require.NoError(t, err)
	require.NoError(t, storage.Delete(testContext(), []Artifact{{Name: "missing", Repository: "library", Tag: "latest"}}))
}
