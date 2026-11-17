package store

import (
	"context"
	"fmt"
	"sync"
	"testing"

	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

// recordingVerifier rejects the digests in reject and records each call.
type recordingVerifier struct {
	mu     sync.Mutex
	reject map[digest.Digest]bool
	calls  []Signatures
}

func (v *recordingVerifier) Verify(_ context.Context, reference string, signatures Signatures) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls = append(v.calls, signatures)
	if v.reject[signatures.Subject.Digest] {
		return fmt.Errorf("%w: %s", ErrSignatureRejected, reference)
	}
	return nil
}

func TestOCIStoreKeepsRejectedImageOutAndCopiesTheRest(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	unsigned := pushTestManifest(t, src.repo, "unsigned", []byte("unsigned-layer"), nil, "")
	verifier := &recordingVerifier{reject: map[digest.Digest]bool{unsigned.Digest: true}}
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	local.SetSignatureVerifier(verifier)

	err = local.Replicate(ctx, src.store, []Artifact{
		{Name: "team/app", Tag: "unsigned", Digest: unsigned.Digest.String()},
		{Name: "team/app", Tag: "latest", Digest: src.image.Digest.String()},
	})

	require.ErrorIs(t, err, ErrSignatureRejected)
	_, err = local.Pull(ctx, Artifact{Name: "team/app", Tag: "unsigned"}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	_, err = local.Pull(ctx, Artifact{Name: "team/app", Tag: "latest"}, PullResourceManifest)
	require.NoError(t, err)

	require.Len(t, verifier.calls, 2)
	signed := verifier.calls[1]
	require.Equal(t, src.image.Digest, signed.Subject.Digest)
	require.Len(t, signed.Tagged, 1)
	require.Equal(t, src.signature.Digest, signed.Tagged[0].Digest)
	require.Len(t, signed.Referrers, 1)
	require.Equal(t, src.referrer.Digest, signed.Referrers[0].Digest)
}

func TestOCIStoreDoesNotVerifySignatureArtifacts(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	verifier := &recordingVerifier{}
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	local.SetSignatureVerifier(verifier)

	// A proxy fill of a cosign tag and of a referrer by digest.
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{
		{Name: "team/app", Tag: supplyChainTag(src.image.Digest, ".sig")},
		{Name: "team/app", Tag: src.referrer.Digest.String()},
	}))
	require.Empty(t, verifier.calls)
}

func TestRegistryStoreKeepsRejectedImageOut(t *testing.T) {
	ctx := context.Background()
	src := newSignedSource(t)
	verifier := &recordingVerifier{reject: map[digest.Digest]bool{src.image.Digest: true}}
	destination := plainRegistry(t)
	destination.SetSignatureVerifier(verifier)

	err := destination.Replicate(ctx, src.store, []Artifact{{Name: "team/app", Tag: "latest", Digest: src.image.Digest.String()}})

	require.ErrorIs(t, err, ErrSignatureRejected)
	_, err = destination.Pull(ctx, Artifact{Name: "team/app", Tag: "latest"}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	_, err = destination.Pull(ctx, Artifact{Name: "team/app", Tag: supplyChainTag(src.image.Digest, ".sig")}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	require.Len(t, verifier.calls, 1)
	require.Len(t, verifier.calls[0].Tagged, 1)
	require.Len(t, verifier.calls[0].Referrers, 1)
}
