package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// defaultIngestMaxAge bounds what a crashed push leaves in ingest/. A live
// push keeps touching its file, so only stalled or orphaned files age out.
const defaultIngestMaxAge = time.Hour

type GCOptions struct {
	// Grace keeps unreferenced content until it has been unreferenced this long.
	Grace time.Duration
	// Roots are kept besides tagged graphs; FillLeases at least.
	Roots []GCRoots
	// IngestMaxAge removes oras ingest/ files older than this; oras never
	// cleans files a crash leaves there. Zero keeps them.
	IngestMaxAge time.Duration
}

type GCStats struct {
	Scanned, Deleted, InUse, InGrace, Leased, IngestRemoved int
	BytesFreed                                              int64
}

// CollectGarbage is mark and sweep from tags plus opts.Roots, honouring the
// grace period and open readers. It replaces oras Store.GC, whose roots are
// tags only and which knows nothing of readers. The grace clock lives in
// memory, so a restart starts every grace period again.
func (s *OCIStore) CollectGarbage(ctx context.Context, opts GCOptions) (GCStats, error) {
	s.gcMu.Lock()
	defer s.gcMu.Unlock()

	var stats GCStats
	leased := map[digest.Digest]struct{}{}
	for _, roots := range opts.Roots {
		if roots == nil {
			continue
		}
		digests, err := roots.Roots(ctx)
		if err != nil {
			return stats, fmt.Errorf("collect GC roots: %w", err)
		}
		for _, d := range digests {
			leased[d] = struct{}{}
		}
	}

	unlock := s.lockIndex("collect")
	defer unlock()
	reachable, err := s.markLocked(ctx)
	if err != nil {
		return stats, fmt.Errorf("mark tagged graphs: %w", err)
	}
	if err := s.sweepLocked(ctx, opts.Grace, reachable, leased, &stats); err != nil {
		return stats, err
	}
	if opts.IngestMaxAge > 0 {
		removed, err := removeStaleIngest(s.root, opts.IngestMaxAge)
		stats.IngestRemoved = removed
		if err != nil {
			return stats, err
		}
	}
	if stats.Deleted > 0 {
		s.provenance.Clear()
		if err := s.reloadLocked(ctx); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func (s *OCIStore) markLocked(ctx context.Context) (map[digest.Digest]struct{}, error) {
	var tags []string
	if err := s.store.Tags(ctx, "", func(page []string) error {
		tags = append(tags, page...)
		return nil
	}); err != nil {
		return nil, err
	}
	reachable := map[digest.Digest]struct{}{}
	for _, tag := range tags {
		root, err := s.store.Resolve(ctx, tag)
		if err != nil {
			return nil, fmt.Errorf("resolve tag %s: %w", tag, err)
		}
		if _, ok := reachable[root.Digest]; ok {
			continue
		}
		err = walkDescriptorGraph(ctx, s.store, root, func(desc ocispec.Descriptor) error {
			reachable[desc.Digest] = struct{}{}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk graph of %s: %w", tag, err)
		}
	}
	return reachable, nil
}

func (s *OCIStore) sweepLocked(ctx context.Context, grace time.Duration, reachable, leased map[digest.Digest]struct{}, stats *GCStats) error {
	blobsRoot := filepath.Join(s.root, ocispec.ImageBlobsDir)
	algorithms, err := os.ReadDir(blobsRoot)
	if err != nil {
		return fmt.Errorf("list blobs: %w", err)
	}
	now := time.Now()
	present := map[digest.Digest]struct{}{}
	for _, alg := range algorithms {
		if !alg.IsDir() || !digest.Algorithm(alg.Name()).Available() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(blobsRoot, alg.Name()))
		if err != nil {
			return fmt.Errorf("list %s blobs: %w", alg.Name(), err)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			d := digest.NewDigestFromEncoded(digest.Algorithm(alg.Name()), entry.Name())
			if d.Validate() != nil {
				continue
			}
			stats.Scanned++
			present[d] = struct{}{}
			if _, ok := reachable[d]; ok {
				delete(s.unreferencedSince, d)
				continue
			}
			if _, ok := leased[d]; ok {
				stats.Leased++
				delete(s.unreferencedSince, d)
				continue
			}
			if s.readers.InUse(d) {
				stats.InUse++
				continue
			}
			first, ok := s.unreferencedSince[d]
			if !ok {
				first = now
				s.unreferencedSince[d] = now
			}
			if now.Sub(first) < grace {
				stats.InGrace++
				continue
			}
			path := filepath.Join(blobsRoot, alg.Name(), entry.Name())
			var size int64
			if info, err := entry.Info(); err == nil {
				size = info.Size()
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete blob %s: %w", d, err)
			}
			delete(s.unreferencedSince, d)
			stats.Deleted++
			stats.BytesFreed += size
		}
	}
	for d := range s.unreferencedSince {
		if _, ok := present[d]; !ok {
			delete(s.unreferencedSince, d)
		}
	}
	return nil
}

func removeStaleIngest(root string, maxAge time.Duration) (int, error) {
	dir := filepath.Join(root, "ingest")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("list ingest: %w", err)
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("remove stale ingest file %s: %w", entry.Name(), err)
		}
		removed++
	}
	return removed, nil
}

// reloadLocked persists the tags and reopens the layout, so oras drops the
// digest entries and graph edges of deleted content; oras Store.GC did that
// by rebuilding its memory index. Stages that hold the old *oci.Store keep
// using it for existence checks, which read the filesystem.
func (s *OCIStore) reloadLocked(ctx context.Context) error {
	index, err := s.buildIndexLocked(ctx)
	if err != nil {
		return err
	}
	if err := WriteIndexAtomic(s.root, index); err != nil {
		return err
	}
	reopened, err := openLayout(s.root, s.opts.AtomicIndex)
	if err != nil {
		return fmt.Errorf("reopen OCI layout after collection: %w", err)
	}
	s.store = reopened
	return nil
}

// RunCollector runs CollectGarbage every interval until ctx ends. Delete
// collects too, but content still in its grace period then waits for the
// next Delete; this frees it without one.
func (s *OCIStore) RunCollector(ctx context.Context, interval time.Duration, opts GCOptions) error {
	if interval <= 0 {
		return errors.New("store collector interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		stats, err := s.CollectGarbage(ctx, opts)
		log := logger.FromContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Warn().Err(err).Msg("OCI store garbage collection failed")
			continue
		}
		if stats.Deleted > 0 || stats.IngestRemoved > 0 {
			log.Info().Int("deleted", stats.Deleted).Int64("bytes_freed", stats.BytesFreed).
				Int("ingest_removed", stats.IngestRemoved).Msg("OCI store garbage collected")
		}
	}
}
