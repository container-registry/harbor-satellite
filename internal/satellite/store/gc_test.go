package store

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// The fill writes untagged content into the serving layout through the
// streaming path (5.3): the manifest, then a streamed layer, and the graph is
// completed and tagged last. Replicate stages its copy outside the layout,
// so only this path can lose content to a collection mid-fill.
func TestCollectGarbage_KeepsInFlightCopy(t *testing.T) {
	ctx := edgeContext(t)
	held := []byte("layer held upstream")
	heldDigest := digest.FromBytes(held)
	release := make(chan struct{})
	heldRequested := make(chan struct{})
	var requestedOnce sync.Once
	address, source := edgeUpstream(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/blobs/"+heldDigest.String()) {
				requestedOnce.Do(func() { close(heldRequested) })
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
	artifact := Artifact{Name: "team/app", Tag: "v1"}

	manifest, _, err := s.FillManifest(ctx, source, artifact)
	require.NoError(t, err)
	first := Artifact{Name: "team/app", Digest: layers[0].Digest.String()}
	firstDesc, err := source.Pull(ctx, first, PullResourceBlob)
	require.NoError(t, err)
	streamed, err := s.StreamBlob(ctx, source, first, firstDesc)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, streamed.Body)
	require.NoError(t, err)
	require.NoError(t, streamed.Body.Close())
	copied := edgeBlobPath(root, layers[0].Digest)
	require.FileExists(t, copied)

	completed := make(chan error, 1)
	go func() { completed <- s.CompleteGraph(ctx, source, artifact, manifest) }()
	select {
	case <-heldRequested:
	case <-time.After(10 * time.Second):
		t.Fatal("the graph completion never asked for the held layer")
	}

	stats, err := s.CollectGarbage(ctx, GCOptions{Grace: 0, Roots: []GCRoots{leases}})
	require.NoError(t, err)
	require.Positive(t, stats.Leased)
	require.FileExists(t, copied)
	require.FileExists(t, edgeBlobPath(root, manifest.Digest))

	releaseHeld()
	require.NoError(t, <-completed)
	_, err = s.Pull(ctx, artifact, PullResourceManifest)
	require.NoError(t, err, "the tag exists once the graph is complete")
	desc, err := s.Pull(ctx, first, PullResourceBlob)
	require.NoError(t, err)
	response, err := s.Fetch(ctx, first, desc, PullResourceBlob, nil)
	require.NoError(t, err)
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, []byte("layer copied first"), payload)
}

func TestCollectGarbage_SkipsBlobWithOpenReader(t *testing.T) {
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

func TestRunCollector_FreesContentAfterGrace(t *testing.T) {
	root := t.TempDir()
	s, _, err := OpenOCIStore(root, OCIStoreOptions{AtomicIndex: true, GCGrace: 30 * time.Millisecond})
	require.NoError(t, err)
	digests := replicateAndUntag(t, s)

	ctx, cancel := context.WithCancel(edgeContext(t))
	done := make(chan error, 1)
	go func() {
		done <- s.RunCollector(ctx, 20*time.Millisecond, GCOptions{Grace: 30 * time.Millisecond})
	}()
	require.Eventually(t, func() bool {
		for _, d := range digests {
			if _, err := os.Stat(edgeBlobPath(root, d)); err == nil {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestDelete_KeepsContentInGraceUntilCollector(t *testing.T) {
	root := t.TempDir()
	s, _, err := OpenOCIStore(root, OCIStoreOptions{AtomicIndex: true, GCGrace: time.Hour})
	require.NoError(t, err)
	ctx := edgeContext(t)
	address, source := edgeUpstream(t, nil)
	manifest, _ := pushEdgeImage(t, address, "team/app", "v1", []byte("graced layer"))
	artifact := Artifact{Name: "team/app", Tag: "v1"}
	require.NoError(t, s.Replicate(ctx, source, []Artifact{artifact}))

	require.NoError(t, s.Delete(ctx, []Artifact{artifact}))
	_, err = s.Pull(ctx, artifact, PullResourceManifest)
	require.Error(t, err, "an untagged artifact must not be pullable while its content waits out the grace period")
	_, err = s.Pull(ctx, Artifact{Name: "team/app", Digest: manifest.Digest.String()}, PullResourceManifest)
	require.Error(t, err)
	require.FileExists(t, edgeBlobPath(root, manifest.Digest))
}
