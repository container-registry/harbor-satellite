package store

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

func TestOCIStoreReplicatesAndStreamsManifestGraph(t *testing.T) {
	remote, manifestPayload, manifestDesc, layer, layerDesc := testArtifact(t, "team/app", "latest")
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	artifact := Artifact{Name: "team/app", Tag: "latest"}

	_, err = local.Pull(context.Background(), artifact, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	require.NoError(t, local.Replicate(context.Background(), remote, []Artifact{artifact}))
	descriptor, err := local.Pull(context.Background(), artifact, PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, manifestDesc, descriptor)
	reader, err := local.Fetch(context.Background(), artifact, descriptor)
	require.NoError(t, err)
	payload, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, manifestPayload, payload)

	blobArtifact := Artifact{Name: "team/app", Digest: layerDesc.Digest.String()}
	blobDescriptor, err := local.Pull(context.Background(), blobArtifact, PullResourceBlob)
	require.NoError(t, err)
	blobReader, err := local.Fetch(context.Background(), blobArtifact, blobDescriptor)
	require.NoError(t, err)
	blobPayload, err := io.ReadAll(blobReader)
	require.NoError(t, err)
	require.NoError(t, blobReader.Close())
	require.Equal(t, layer, blobPayload)
}

func TestOCIStoreStandaloneBlobReplication(t *testing.T) {
	remote, _, _, layer, layerDesc := testArtifact(t, "team/app", "latest")
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	artifact := Artifact{Name: "team/app", Digest: layerDesc.Digest.String()}
	require.NoError(t, local.Replicate(context.Background(), remote, []Artifact{artifact}))
	descriptor, err := local.Pull(context.Background(), artifact, PullResourceBlob)
	require.NoError(t, err)
	reader, err := local.Fetch(context.Background(), artifact, descriptor)
	require.NoError(t, err)
	payload, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, layer, payload)
}

func testArtifact(t *testing.T, repository, tag string) (Store, []byte, ocispec.Descriptor, []byte, ocispec.Descriptor) {
	t.Helper()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")
	repo, err := orasremote.NewRepository(address + "/" + repository)
	require.NoError(t, err)
	repo.PlainHTTP = true
	layer := []byte("satellite-streamed-layer")
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", layer)
	configPayload := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
	manifestPayload, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
		Config: configDesc, Layers: []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	require.NoError(t, repo.Push(context.Background(), layerDesc, bytes.NewReader(layer)))
	require.NoError(t, repo.Push(context.Background(), configDesc, bytes.NewReader(configPayload)))
	require.NoError(t, repo.PushReference(context.Background(), manifestDesc, bytes.NewReader(manifestPayload), tag))
	storage, err := NewRegistryStore(RegistryOptions{Endpoint: address, PlainHTTP: true})
	require.NoError(t, err)
	return storage, manifestPayload, manifestDesc, layer, layerDesc
}
