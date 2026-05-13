package state

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"
)

// TestReplicate_PreservesMultiArchIndex pushes a multi-arch index to the source,
// runs the replicator, and asserts the destination preserves the index media type
// rather than flattening to a single platform.
func TestReplicate_PreservesMultiArchIndex(t *testing.T) {
	_, srcAddr := newTestRegistry(t)
	_, dstAddr := newTestRegistry(t)

	// Build a 3-platform OCI image index.
	idx, err := random.Index(1024, 2, 3)
	require.NoError(t, err)

	srcRef, err := name.ParseReference(srcAddr+"/library/multiarch:latest", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(srcRef, idx))

	// Sanity: source must actually be an index for this test to be meaningful.
	srcDesc, err := remote.Get(srcRef)
	require.NoError(t, err)
	require.True(t, srcDesc.MediaType.IsIndex(),
		"fixture broken: source is not an index, got %s", srcDesc.MediaType)

	r := NewBasicReplicator("", "", srcAddr, dstAddr, "", "", true)
	ctx := testContext()

	require.NoError(t, r.Replicate(ctx, []Entity{
		{Name: "multiarch", Repository: "library", Tag: "latest"},
	}))

	dstRef, err := name.ParseReference(dstAddr+"/library/multiarch:latest", name.Insecure)
	require.NoError(t, err)
	dstDesc, err := remote.Get(dstRef)
	require.NoError(t, err)

	require.True(t, dstDesc.MediaType.IsIndex(),
		"expected destination to preserve multi-arch index, got %s", dstDesc.MediaType)
	require.Equal(t, srcDesc.Digest, dstDesc.Digest,
		"expected destination index digest to match source (byte-for-byte preservation)")
}
