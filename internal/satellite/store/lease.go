package store

import (
	"context"
	"sync"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// GCRoots supplies digests CollectGarbage keeps in addition to tagged graphs.
type GCRoots interface {
	Roots(ctx context.Context) ([]digest.Digest, error)
}

// FillLeases records content that in-flight fills wrote into the serving
// layout but have not tagged yet. Tags are the only other GC roots, so
// without a lease a collection would delete the start of a running fill.
type FillLeases struct {
	mu     sync.Mutex
	next   uint64
	active map[uint64]map[digest.Digest]struct{}
}

var _ GCRoots = (*FillLeases)(nil)

func NewFillLeases() *FillLeases {
	return &FillLeases{active: map[uint64]map[digest.Digest]struct{}{}}
}

func (l *FillLeases) Begin(_ Artifact) *FillLease {
	if l == nil {
		return &FillLease{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	l.active[l.next] = map[digest.Digest]struct{}{}
	return &FillLease{owner: l, id: l.next}
}

func (l *FillLeases) Roots(_ context.Context) ([]digest.Digest, error) {
	if l == nil {
		return nil, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[digest.Digest]struct{}{}
	var roots []digest.Digest
	for _, digests := range l.active {
		for d := range digests {
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			roots = append(roots, d)
		}
	}
	return roots, nil
}

// FillLease is one artifact's fill; End releases its content to normal GC.
type FillLease struct {
	owner *FillLeases
	id    uint64
}

// Track must be called before desc is written, so a collection that lists
// the blob after it lands also sees the lease.
func (f *FillLease) Track(desc ocispec.Descriptor) {
	if f == nil || f.owner == nil {
		return
	}
	f.owner.mu.Lock()
	defer f.owner.mu.Unlock()
	if digests, ok := f.owner.active[f.id]; ok {
		digests[desc.Digest] = struct{}{}
	}
}

func (f *FillLease) End() {
	if f == nil || f.owner == nil {
		return
	}
	f.owner.mu.Lock()
	defer f.owner.mu.Unlock()
	delete(f.owner.active, f.id)
}
