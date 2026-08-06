package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

// ociDigest returns the digest the replicator will push to the destination for
// img, i.e. after the lazy OCI media type conversion.
func ociDigest(t *testing.T, img v1.Image) v1.Hash {
	t.Helper()
	d, err := mutate.MediaType(img, types.OCIManifestSchema1).Digest()
	require.NoError(t, err)
	return d
}

func digestOf(t *testing.T, img v1.Image) string {
	t.Helper()
	d, err := img.Digest()
	require.NoError(t, err)
	return d.String()
}

// TestReplicate_PullsPinnedDigestAfterTagMoved is the core regression test:
// the tag is repointed at different content after Ground Control recorded the
// digest, and the satellite must still replicate the recorded content.
func TestReplicate_PullsPinnedDigestAfterTagMoved(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	desired := pushImage(t, srcAddr, "alpine", "latest", 2)

	// Attacker moves the tag to different content before the satellite pulls.
	substituted := pushImage(t, srcAddr, "alpine", "latest", 3)
	require.NotEqual(t, digestOf(t, desired), digestOf(t, substituted))

	r := NewBasicReplicator("", "", srcAddr, dstAddr, "", "", true)

	err := r.Replicate(testContext(), []Entity{
		{Name: "alpine", Repository: "library", Tag: "latest", Digest: digestOf(t, desired)},
	})
	require.NoError(t, err)

	dstRef, err := name.ParseReference(dstAddr+"/library/alpine:latest", name.Insecure)
	require.NoError(t, err)
	dstDesc, err := remote.Head(dstRef)
	require.NoError(t, err)

	require.Equal(t, ociDigest(t, desired), dstDesc.Digest, "destination must hold the digest from the desired state")
	require.NotEqual(t, ociDigest(t, substituted), dstDesc.Digest, "moved tag must not be replicated")
}

// TestReplicate_FailsWhenPinnedDigestAbsent ensures we never silently fall back
// to the tag when the recorded digest is no longer served.
func TestReplicate_FailsWhenPinnedDigestAbsent(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	pushImage(t, srcAddr, "alpine", "latest", 2)

	absent, err := random.Image(1024, 1)
	require.NoError(t, err)

	r := NewBasicReplicator("", "", srcAddr, dstAddr, "", "", true)

	err = r.Replicate(testContext(), []Entity{
		{Name: "alpine", Repository: "library", Tag: "latest", Digest: digestOf(t, absent)},
	})
	require.Error(t, err)

	dstRef, err := name.ParseReference(dstAddr+"/library/alpine:latest", name.Insecure)
	require.NoError(t, err)
	_, err = remote.Head(dstRef)
	require.Error(t, err, "nothing must be published when the pinned digest is unavailable")
}

func TestReplicate_MalformedDigestIsRejected(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	pushImage(t, srcAddr, "alpine", "latest", 1)

	r := NewBasicReplicator("", "", srcAddr, dstAddr, "", "", true)

	err := r.Replicate(testContext(), []Entity{
		{Name: "alpine", Repository: "library", Tag: "latest", Digest: "not-a-digest"},
	})
	require.Error(t, err)
}

// TestPinnedSourceRef_ResolvesTagOnce covers the no-digest path: the tag is
// resolved to a digest once, and a later tag move cannot change what that
// reference fetches.
func TestPinnedSourceRef_ResolvesTagOnce(t *testing.T) {
	srcAddr := newTestRegistry(t)

	original := pushImage(t, srcAddr, "nginx", "1.25", 2)

	entity := Entity{Name: "nginx", Repository: "library", Tag: "1.25"}
	nameOpts := []name.Option{name.Insecure}

	resolved, err := pinnedSourceRef(srcAddr, entity, nameOpts, nil)
	require.NoError(t, err)
	require.Equal(t, digestOf(t, original), resolved.DigestStr())

	// Tag moves after resolution.
	moved := pushImage(t, srcAddr, "nginx", "1.25", 3)
	require.NotEqual(t, digestOf(t, original), digestOf(t, moved))

	desc, err := remote.Get(resolved)
	require.NoError(t, err)
	require.Equal(t, digestOf(t, original), desc.Digest.String())
}

func TestPinnedSourceRef_UsesDesiredStateDigest(t *testing.T) {
	entity := Entity{
		Name:       "alpine",
		Repository: "library",
		Tag:        "latest",
		Digest:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

	ref, err := pinnedSourceRef("registry.example.com", entity, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "registry.example.com/library/alpine@"+entity.Digest, ref.String())
}

func TestVerifyFetchedDigest(t *testing.T) {
	const want = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	const other = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	ref, err := name.NewDigest("registry.example.com/library/alpine@" + want)
	require.NoError(t, err)

	require.NoError(t, verifyFetchedDigest(ref, want))

	err = verifyFetchedDigest(ref, other)
	require.Error(t, err)
	require.Contains(t, err.Error(), "digest mismatch")
}

// TestDeliver_PullsPinnedDigestAfterTagMoved is the direct-delivery equivalent
// of the replicator regression test.
func TestDeliver_PullsPinnedDigestAfterTagMoved(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dir := t.TempDir()

	desired := pushImage(t, srcAddr, "alpine", "latest", 2)
	substituted := pushImage(t, srcAddr, "alpine", "latest", 3)

	d := NewDirectDeliverer(dir, "", "", srcAddr, true)

	entity := Entity{Name: "alpine", Repository: "library", Tag: "latest", Digest: digestOf(t, desired)}
	require.NoError(t, d.Deliver(testContext(), []Entity{entity}))

	path := filepath.Join(dir, tarballFilename(entity))
	written, err := tarball.ImageFromPath(path, nil)
	require.NoError(t, err)

	desiredConfig, err := desired.ConfigName()
	require.NoError(t, err)
	substitutedConfig, err := substituted.ConfigName()
	require.NoError(t, err)
	writtenConfig, err := written.ConfigName()
	require.NoError(t, err)

	require.Equal(t, desiredConfig, writtenConfig, "tarball must hold the digest from the desired state")
	require.NotEqual(t, substitutedConfig, writtenConfig, "moved tag must not be delivered")

	require.Equal(t, digestOf(t, desired), d.loadDigestMap()[tarballFilename(entity)])
}

// TestDeliver_SkipsUnchangedTagOnlyEntity covers entities whose desired state
// carries no digest. Their digest is only known once the tag is resolved, so
// without a check against the resolved digest the tarball is rewritten on every
// sync cycle even when the content has not changed.
func TestDeliver_SkipsUnchangedTagOnlyEntity(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dir := t.TempDir()

	pushImage(t, srcAddr, "alpine", "latest", 2)

	d := NewDirectDeliverer(dir, "", "", srcAddr, true)

	// No Digest: the tag has to be resolved to learn the content identity.
	entity := Entity{Name: "alpine", Repository: "library", Tag: "latest"}
	path := filepath.Join(dir, tarballFilename(entity))

	require.NoError(t, d.Deliver(testContext(), []Entity{entity}))
	require.FileExists(t, path, "tarball must be written on the first delivery")

	// Backdate the tarball so a rewrite is unambiguous: writeAtomically renames a
	// fresh temp file into place, so any re-delivery resets the mtime to now.
	// Comparing against a backdated stamp avoids depending on filesystem
	// timestamp granularity.
	backdated := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(path, backdated, backdated))

	// Second cycle, content unchanged: the tarball must not be rewritten.
	require.NoError(t, d.Deliver(testContext(), []Entity{entity}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.ModTime().Equal(backdated),
		"unchanged tag-only entity must not be re-delivered (mtime %s, want %s)", info.ModTime(), backdated)
}

func TestDeliver_SkipsWhenPinnedDigestAbsent(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dir := t.TempDir()

	pushImage(t, srcAddr, "alpine", "latest", 2)

	absent, err := random.Image(1024, 1)
	require.NoError(t, err)

	d := NewDirectDeliverer(dir, "", "", srcAddr, true)

	entity := Entity{Name: "alpine", Repository: "library", Tag: "latest", Digest: digestOf(t, absent)}
	require.NoError(t, d.Deliver(testContext(), []Entity{entity}))

	_, err = os.Stat(filepath.Join(dir, tarballFilename(entity)))
	require.True(t, os.IsNotExist(err), "no tarball must be written when the pinned digest is unavailable")
	require.Empty(t, d.loadDigestMap())
}
