package store

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

// replicateAndUntag leaves the graph of team/app:v1 in the store with no
// tag pointing at it, which is what collection decides about.
func replicateAndUntag(t *testing.T, s *OCIStore) []digest.Digest {
	t.Helper()
	ctx := edgeContext(t)
	address, source := edgeUpstream(t, nil)
	manifest, layers := pushEdgeImage(t, address, "team/app", "v1", []byte("collectable layer"))
	artifact := Artifact{Name: "team/app", Tag: "v1"}
	require.NoError(t, s.Replicate(ctx, source, []Artifact{artifact}))
	require.NoError(t, s.store.Untag(ctx, artifact.Reference()))
	return []digest.Digest{manifest.Digest, layers[0].Digest}
}

func TestCollectGarbage_KeepsInFlightCopy(t *testing.T) {
	pending.Plan(t, "5.2", "oras GC roots are tags only and delete the layers of a copy in flight")
	ctx := edgeContext(t)
	held := []byte("layer held upstream")
	heldDigest := digest.FromBytes(held)
	release := make(chan struct{})
	address, source := edgeUpstream(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/blobs/"+heldDigest.String()) {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	// Runs before the server's Close, which waits for the held request.
	releaseHeld := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseHeld)
	_, layers := pushEdgeImage(t, address, "team/app", "v1", []byte("layer copied first"), held)
	root := t.TempDir()
	leases := NewFillLeases()
	s, _, err := OpenOCIStore(root, OCIStoreOptions{AtomicIndex: true, FillLeases: leases})
	require.NoError(t, err)

	replicated := make(chan error, 1)
	go func() { replicated <- s.Replicate(ctx, source, []Artifact{{Name: "team/app", Tag: "v1"}}) }()
	copied := edgeBlobPath(root, layers[0].Digest)
	require.Eventually(t, func() bool { _, err := os.Stat(copied); return err == nil }, 10*time.Second, 10*time.Millisecond)

	stats, err := s.CollectGarbage(ctx, GCOptions{Grace: 0, Roots: []GCRoots{leases}})
	require.NoError(t, err)
	require.Positive(t, stats.Leased)
	require.FileExists(t, copied)

	releaseHeld()
	require.NoError(t, <-replicated)
	blob := Artifact{Name: "team/app", Digest: layers[0].Digest.String()}
	desc, err := s.Pull(ctx, blob, PullResourceBlob)
	require.NoError(t, err)
	response, err := s.Fetch(ctx, blob, desc, PullResourceBlob, nil)
	require.NoError(t, err)
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, []byte("layer copied first"), payload)
}

func TestCollectGarbage_SkipsBlobWithOpenReader(t *testing.T) {
	pending.Plan(t, "5.2", "garbage collection does not track open readers")
	ctx := edgeContext(t)
	root := t.TempDir()
	s, err := NewOCIStore(root)
	require.NoError(t, err)
	address, source := edgeUpstream(t, nil)
	_, layers := pushEdgeImage(t, address, "team/app", "v1", []byte("layer being served"))
	artifact := Artifact{Name: "team/app", Tag: "v1"}
	require.NoError(t, s.Replicate(ctx, source, []Artifact{artifact}))
	blob := Artifact{Name: "team/app", Digest: layers[0].Digest.String()}
	desc, err := s.Pull(ctx, blob, PullResourceBlob)
	require.NoError(t, err)
	response, err := s.Fetch(ctx, blob, desc, PullResourceBlob, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.NoError(t, s.store.Untag(ctx, artifact.Reference()))

	stats, err := s.CollectGarbage(ctx, GCOptions{Grace: 0})
	require.NoError(t, err)
	require.Positive(t, stats.InUse)
	require.FileExists(t, edgeBlobPath(root, layers[0].Digest))
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, []byte("layer being served"), payload)
	require.NoError(t, response.Body.Close())

	stats, err = s.CollectGarbage(ctx, GCOptions{Grace: 0})
	require.NoError(t, err)
	require.Positive(t, stats.Deleted)
	require.NoFileExists(t, edgeBlobPath(root, layers[0].Digest))
}

func TestCollectGarbage_RespectsGrace(t *testing.T) {
	pending.Plan(t, "5.2", "garbage collection has no grace period")
	root := t.TempDir()
	s, err := NewOCIStore(root)
	require.NoError(t, err)
	digests := replicateAndUntag(t, s)

	stats, err := s.CollectGarbage(edgeContext(t), GCOptions{Grace: time.Hour})
	require.NoError(t, err)
	require.Positive(t, stats.InGrace)
	require.Zero(t, stats.Deleted)
	for _, d := range digests {
		require.FileExists(t, edgeBlobPath(root, d))
	}
}

func TestCollectGarbage_DeletesAfterGrace(t *testing.T) {
	pending.Plan(t, "5.2", "garbage collection has no grace period")
	ctx := edgeContext(t)
	root := t.TempDir()
	s, err := NewOCIStore(root)
	require.NoError(t, err)
	digests := replicateAndUntag(t, s)
	opts := GCOptions{Grace: 50 * time.Millisecond}

	_, err = s.CollectGarbage(ctx, opts)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	stats, err := s.CollectGarbage(ctx, opts)
	require.NoError(t, err)
	require.GreaterOrEqual(t, stats.Deleted, len(digests))
	require.Positive(t, stats.BytesFreed)
	for _, d := range digests {
		require.NoFileExists(t, edgeBlobPath(root, d))
	}
}

func TestCollectGarbage_RemovesStaleIngest(t *testing.T) {
	pending.Plan(t, "5.2", "oras never removes ingest files a crash leaves behind")
	root := t.TempDir()
	s, err := NewOCIStore(root)
	require.NoError(t, err)
	ingest := filepath.Join(root, "ingest")
	require.NoError(t, os.MkdirAll(ingest, 0o750))
	stale, fresh := filepath.Join(ingest, "stale_1"), filepath.Join(ingest, "fresh_1")
	require.NoError(t, os.WriteFile(stale, []byte("partial"), 0o600))
	require.NoError(t, os.WriteFile(fresh, []byte("partial"), 0o600))
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	stats, err := s.CollectGarbage(edgeContext(t), GCOptions{Grace: time.Hour, IngestMaxAge: time.Hour})
	require.NoError(t, err)
	require.Equal(t, 1, stats.IngestRemoved)
	require.NoFileExists(t, stale)
	require.FileExists(t, fresh)
}

func TestRefCounter_TracksReaders(t *testing.T) {
	pending.Plan(t, "5.2", "garbage collection does not track open readers")
	r := NewRefCounter()
	d := digest.FromString("layer")
	releaseFirst := r.Acquire(d)
	releaseSecond := r.Acquire(d)
	require.True(t, r.InUse(d))

	releaseFirst()
	require.True(t, r.InUse(d))
	releaseSecond()
	require.False(t, r.InUse(d))
	require.False(t, r.InUse(digest.FromString("other")))
}
