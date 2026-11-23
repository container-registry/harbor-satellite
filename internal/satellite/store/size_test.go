package store

import (
	"context"
	"encoding/json"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestMissingBytesIgnoresBlobsAlreadyPresent(t *testing.T) {
	remote, manifestPayload, manifestDesc, _, _ := testArtifact(t)
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	artifact := Artifact{Name: "team/app", Tag: "latest", Digest: manifestDesc.Digest.String()}

	var manifest ocispec.Manifest
	require.NoError(t, json.Unmarshal(manifestPayload, &manifest))
	expected := int64(len(manifestPayload)) + manifest.Config.Size
	for _, layer := range manifest.Layers {
		expected += layer.Size
	}
	missing, err := MissingBytes(context.Background(), local, remote, artifact)
	require.NoError(t, err)
	require.Equal(t, expected, missing)

	require.NoError(t, local.Replicate(context.Background(), remote, []Artifact{artifact}))
	missing, err = MissingBytes(context.Background(), local, remote, artifact)
	require.NoError(t, err)
	require.Equal(t, int64(0), missing)
}

func TestMissingBytesRejectsAnUnsizedStore(t *testing.T) {
	_, err := MissingBytes(context.Background(), nil, nil, Artifact{Name: "app", Tag: "latest"})
	require.ErrorIs(t, err, ErrUnsized)
}
