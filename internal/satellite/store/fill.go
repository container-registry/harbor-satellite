package store

import (
	"context"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Filler is the streaming pull-through of plan 5.3, an optional capability
// next to Store.
type Filler interface {
	// FillManifest stores the manifest without its graph and returns it.
	FillManifest(ctx context.Context, source Store, artifact Artifact) (ocispec.Descriptor, []byte, error)
	// StreamBlob serves descriptor from source while writing it to the store;
	// the store commits the blob only after the digest verifies.
	StreamBlob(ctx context.Context, source Store, artifact Artifact, descriptor ocispec.Descriptor) (*http.Response, error)
	// CompleteGraph copies what is missing below root, then tags the artifact.
	CompleteGraph(ctx context.Context, source Store, artifact Artifact, root ocispec.Descriptor) error
}

var _ Filler = (*OCIStore)(nil)

func (s *OCIStore) FillManifest(ctx context.Context, source Store, artifact Artifact) (ocispec.Descriptor, []byte, error) {
	return ocispec.Descriptor{}, nil, notimpl.Err("5.3 fill manifest")
}

func (s *OCIStore) StreamBlob(ctx context.Context, source Store, artifact Artifact, descriptor ocispec.Descriptor) (*http.Response, error) {
	return nil, notimpl.Err("5.3 stream blob")
}

func (s *OCIStore) CompleteGraph(ctx context.Context, source Store, artifact Artifact, root ocispec.Descriptor) error {
	return notimpl.Err("5.3 complete graph")
}
