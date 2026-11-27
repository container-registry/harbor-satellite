package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMissingBytesIgnoresBlobsAlreadyPresent(t *testing.T) {
	remote, _, manifestDesc, _, layerDesc := testArtifact(t)
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	artifact := Artifact{Name: "team/app", Tag: "latest", Digest: manifestDesc.Digest.String()}

	missing, err := MissingBytes(context.Background(), local, remote, artifact)
	require.NoError(t, err)
	require.GreaterOrEqual(t, missing, layerDesc.Size)

	require.NoError(t, local.Replicate(context.Background(), remote, []Artifact{artifact}))
	missing, err = MissingBytes(context.Background(), local, remote, artifact)
	require.NoError(t, err)
	require.Equal(t, int64(0), missing)
}

func TestMissingBytesRejectsAnUnsizedStore(t *testing.T) {
	_, err := MissingBytes(context.Background(), nil, nil, Artifact{Name: "app", Tag: "latest"})
	require.ErrorIs(t, err, ErrUnsized)
}
