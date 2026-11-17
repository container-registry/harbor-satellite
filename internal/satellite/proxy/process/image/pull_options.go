package image

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/lane"
	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
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
	// whole-graph fill before the first byte.
	Streaming  bool
	Prefetcher Prefetcher
	// Recorder receives pull durations and pull-through fills; nil records
	// nothing.
	Recorder telemetry.Recorder
	// Journal receives one record per manifest filled from Harbor (plan
	// item 6.4); nil journals nothing. It must not block.
	Journal PullThroughJournal
}

// NewPullWithOptions is NewPull with the plan 5.3 pull-through options; zero
// options give exactly NewPull. Streaming needs a local store that
// implements store.Filler.
func NewPullWithOptions(lifecycleCtx context.Context, mode proxy.Mode, localStore, remoteStore store.Store, opts PullOptions) (proxy.HandlerFunc, error) {
	if lifecycleCtx == nil {
		return nil, errors.New("image.NewPull: lifecycle context is required")
	}
	handler := &pullHandler{
		lifecycleCtx: lifecycleCtx,
		mode:         mode,
		localStore:   localStore,
		remoteStore:  remoteStore,
		prefetcher:   opts.Prefetcher,
		recorder:     opts.Recorder,
		journal:      opts.Journal,
	}
	if handler.recorder == nil {
		handler.recorder = telemetry.Nop{}
	}
	if opts.Streaming {
		filler, ok := localStore.(store.Filler)
		if !ok {
			return nil, fmt.Errorf("image.NewPull: streaming pull-through needs a local store that implements store.Filler, got %T", localStore)
		}
		handler.filler = filler
	}
	return handler.handle, nil
}

// NewLanePrefetcher prefetches through l, so prefetch never competes with
// client pulls beyond the lane's concurrency. One prefetch per root and
// reference runs at a time; a full lane drops the request, and the next
// pull of the manifest asks again.
func NewLanePrefetcher(l *lane.Lane, localStore, remoteStore store.Store) (Prefetcher, error) {
	if l == nil {
		return nil, errors.New("image.NewLanePrefetcher: lane is required")
	}
	filler, ok := localStore.(store.Filler)
	if !ok {
		return nil, fmt.Errorf("image.NewLanePrefetcher: local store %T does not implement store.Filler", localStore)
	}
	if remoteStore == nil {
		return nil, errors.New("image.NewLanePrefetcher: remote store is required")
	}
	return &lanePrefetcher{lane: l, filler: filler, remote: remoteStore}, nil
}

type lanePrefetcher struct {
	lane     *lane.Lane
	filler   store.Filler
	remote   store.Store
	inFlight sync.Map
}

func (p *lanePrefetcher) Prefetch(ctx context.Context, artifact store.Artifact, root ocispec.Descriptor) error {
	key := artifact.Name + "\x00" + artifact.Tag + "\x00" + root.Digest.String()
	if _, running := p.inFlight.LoadOrStore(key, struct{}{}); running {
		return nil
	}
	err := p.lane.Submit(ctx, func(jobCtx context.Context) error {
		defer p.inFlight.Delete(key)
		return p.filler.CompleteGraph(jobCtx, p.remote, artifact, root)
	})
	if err != nil {
		p.inFlight.Delete(key)
	}
	return err
}
