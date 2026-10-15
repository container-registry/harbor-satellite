package image

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/satellite/lane"
	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Prefetcher completes, in the background, the graph below a manifest that
// streaming pull-through answered before its blobs were local.
type Prefetcher interface {
	Prefetch(ctx context.Context, artifact store.Artifact, root ocispec.Descriptor) error
}

type PullOptions struct {
	// Streaming (PROXY_STREAMING) answers a manifest miss once the manifest
	// is stored and streams a blob miss while storing it. False keeps the
	// whole-graph fill before the first byte (pull.go:22,78,97).
	Streaming  bool
	Prefetcher Prefetcher
}

// NewPullWithOptions is NewPull with the plan 5.3 pull-through options; zero
// options give exactly NewPull.
func NewPullWithOptions(lifecycleCtx context.Context, mode proxy.Mode, localStore, remoteStore store.Store, opts PullOptions) (proxy.HandlerFunc, error) {
	if !opts.Streaming && opts.Prefetcher == nil {
		return NewPull(lifecycleCtx, mode, localStore, remoteStore)
	}
	return nil, notimpl.Err("5.3 streaming pull-through")
}

// NewLanePrefetcher prefetches through l, so prefetch never competes with
// client pulls beyond the lane's concurrency.
func NewLanePrefetcher(l *lane.Lane, localStore, remoteStore store.Store) (Prefetcher, error) {
	return nil, notimpl.Err("5.3 prefetch lane")
}
