package state

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/ready"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
)

func TestNoteDesiredBlobsRecordsAMissingLayer(t *testing.T) {
	dir := t.TempDir()
	local, manifestDesc, layerDesc := seedLocalManifest(t, dir)
	_, process := newDegradedProcess(t, dir, filepath.Join(dir, "state.json"), "", false, &degradedStubStore{})
	gate := ready.New()
	process.SetImageGate(gate)
	image := Entity{
		Repository: "library",
		Name:       "alpine",
		Tag:        "latest",
		Digest:     manifestDesc.Digest.String(),
	}
	log := zerolog.Nop()

	process.noteDesiredBlobs(context.Background(), local, &degradedStubStore{}, "group", []Entity{image}, &log)

	require.True(t, gate.WatchesBlob(layerDesc.Digest.String()))
	require.True(t, gate.WatchesBlob(manifestDesc.Digest.String()))
	require.False(t, gate.WatchesBlob("sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"))
}

func TestNoteDesiredBlobsKeepsMembershipWhenTheWalkFails(t *testing.T) {
	dir := t.TempDir()
	_, process := newDegradedProcess(t, dir, filepath.Join(dir, "state.json"), "", false, &degradedStubStore{})
	gate := ready.New()
	process.SetImageGate(gate)
	layer := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	gate.SetBlobs("group", []string{layer})
	log := zerolog.Nop()

	process.noteDesiredBlobs(context.Background(), &degradedStubStore{}, &degradedStubStore{}, "group", []Entity{alpineEntity()}, &log)

	require.True(t, gate.WatchesBlob(layer))

	process.noteDesiredBlobs(context.Background(), &degradedStubStore{}, &degradedStubStore{}, "group", nil, &log)
	require.False(t, gate.WatchesBlob(layer))
}

func seedLocalManifest(t *testing.T, root string) (*store.OCIStore, ocispec.Descriptor, ocispec.Descriptor) {
	t.Helper()
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", []byte("satellite-layer"))
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, []byte(`{"architecture":"amd64","os":"linux"}`))
	manifestPayload, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	layout, err := oci.New(root)
	require.NoError(t, err)
	require.NoError(t, layout.Push(context.Background(), manifestDesc, bytes.NewReader(manifestPayload)))
	require.NoError(t, layout.Tag(context.Background(), manifestDesc, "library/alpine:latest"))
	local, err := store.NewOCIStore(root)
	require.NoError(t, err)
	return local, manifestDesc, layerDesc
}
