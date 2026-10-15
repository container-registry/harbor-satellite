package store

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type GCOptions struct {
	// Grace keeps unreferenced content until it has been unreferenced this long.
	Grace time.Duration
	// Roots are kept besides tagged graphs; FillLeases at least.
	Roots []GCRoots
	// IngestMaxAge removes oras ingest/ files older than this; oras never
	// cleans files a crash leaves there.
	IngestMaxAge time.Duration
}

type GCStats struct {
	Scanned, Deleted, InUse, InGrace, Leased, IngestRemoved int
	BytesFreed                                              int64
}

// CollectGarbage is mark and sweep from tags plus opts.Roots, honouring the
// grace period and open readers. OCIStore.Delete runs it in place of oras
// Store.GC (oci.go:474), whose roots ignore in-flight copies.
func (s *OCIStore) CollectGarbage(ctx context.Context, opts GCOptions) (GCStats, error) {
	return GCStats{}, notimpl.Err("5.2 mark and sweep")
}
