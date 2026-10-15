package store

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// GCRoots supplies digests CollectGarbage keeps in addition to tagged graphs.
type GCRoots interface {
	Roots(ctx context.Context) ([]digest.Digest, error)
}

// FillLeases records content that in-flight copies pushed but have not
// tagged yet: oras.Copy tags the root last (copy.go:476-479), and oras GC
// roots are tags only, so it deletes the layers of a running copy.
type FillLeases struct{}

var _ GCRoots = (*FillLeases)(nil)

func NewFillLeases() *FillLeases {
	return &FillLeases{}
}

func (l *FillLeases) Begin(artifact Artifact) *FillLease {
	return &FillLease{}
}

func (l *FillLeases) Roots(ctx context.Context) ([]digest.Digest, error) {
	return nil, notimpl.Err("5.2 fill lease roots")
}

// FillLease is one artifact's copy; End releases its content to normal GC.
type FillLease struct{}

// Track is called from oras CopyGraphOptions PreCopy and OnCopySkipped for
// every descriptor of the fill.
func (f *FillLease) Track(desc ocispec.Descriptor) {}

func (f *FillLease) End() {}
