package image

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/singleflight"
	"oras.land/oras-go/v2/errdef"
)

// manifestFillTimeout bounds one manifest fill; the graph below it is the
// prefetch lane's job.
const manifestFillTimeout = 2 * time.Minute

type filledManifest struct {
	descriptor ocispec.Descriptor
	payload    []byte
}

// handleStreaming answers a GET in proxy mode without the whole-graph fill:
// a manifest miss returns once the manifest is stored, a blob miss streams.
func (p *pullHandler) handleStreaming(request *proxy.Request, artifact store.Artifact, resource store.PullResource, identifier string) error {
	ctx := request.Context()
	descriptor, err := p.localStore.Pull(ctx, artifact, resource)
	if err == nil {
		return p.serveLocal(request, artifact, resource, descriptor)
	}
	if !errors.Is(err, errdef.ErrNotFound) {
		return mapPullError(resource, err)
	}
	if resource == store.PullResourceManifest {
		return p.streamManifest(request, artifact, identifier)
	}
	return p.streamBlob(request, artifact)
}

func (p *pullHandler) serveLocal(request *proxy.Request, artifact store.Artifact, resource store.PullResource, descriptor ocispec.Descriptor) error {
	response, err := p.localStore.Fetch(request.Context(), artifact, descriptor, resource, request.HTTPRequest().Header) //nolint:bodyclose // WriteContent owns and closes the content response body.
	if err != nil {
		return mapPullError(resource, err)
	}
	return request.WriteContent(response)
}

func (p *pullHandler) streamManifest(request *proxy.Request, artifact store.Artifact, identifier string) error {
	key := pullKey(store.PullResourceManifest, artifact.Name, identifier) + "\x00stream"
	resultChannel := p.operations.DoChan(key, func() (any, error) {
		fillCtx, cancel := context.WithTimeout(p.lifecycleCtx, manifestFillTimeout)
		defer cancel()
		descriptor, payload, err := p.filler.FillManifest(fillCtx, p.remoteStore, artifact)
		if err != nil {
			return nil, err
		}
		if p.prefetcher != nil {
			if err := p.prefetcher.Prefetch(p.lifecycleCtx, artifact, descriptor); err != nil {
				logger.FromContext(p.lifecycleCtx).Warn().Err(err).Str("reference", artifact.Reference()).
					Msg("pull-through prefetch not queued; the next manifest pull asks again")
			}
		}
		return filledManifest{descriptor: descriptor, payload: payload}, nil
	})
	var result singleflight.Result
	select {
	case <-request.Context().Done():
		return request.Context().Err()
	case result = <-resultChannel:
	}
	if result.Err != nil {
		return mapPullError(store.PullResourceManifest, result.Err)
	}
	filled, ok := result.Val.(filledManifest)
	if !ok {
		return errors.New("proxy image pull returned an invalid manifest fill")
	}
	return writePullResponse(request, filled.descriptor, &bytesReadSeekCloser{Reader: bytes.NewReader(filled.payload)})
}

func (p *pullHandler) streamBlob(request *proxy.Request, artifact store.Artifact) error {
	ctx := request.Context()
	descriptor, err := p.remoteStore.Pull(ctx, artifact, store.PullResourceBlob)
	if err != nil {
		return mapPullError(store.PullResourceBlob, err)
	}
	response, err := p.filler.StreamBlob(ctx, p.remoteStore, artifact, descriptor)
	if errors.Is(err, store.ErrUnknownSize) {
		return p.fillWholeBlob(request, artifact)
	}
	if err != nil {
		return mapPullError(store.PullResourceBlob, err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return errors.New("proxy image pull: blob stream did not answer 200")
	}
	return request.WriteContent(response)
}

// fillWholeBlob is the pre-5.3 path for a blob whose size the upstream does
// not state: copy it, then serve it locally.
func (p *pullHandler) fillWholeBlob(request *proxy.Request, artifact store.Artifact) error {
	fillCtx, cancel := context.WithTimeout(p.lifecycleCtx, pullFillTimeout)
	defer cancel()
	if err := p.localStore.Replicate(fillCtx, p.remoteStore, []store.Artifact{artifact}); err != nil {
		return mapPullError(store.PullResourceBlob, err)
	}
	descriptor, err := p.localStore.Pull(request.Context(), artifact, store.PullResourceBlob)
	if err != nil {
		return mapPullError(store.PullResourceBlob, err)
	}
	return p.serveLocal(request, artifact, store.PullResourceBlob, descriptor)
}

type bytesReadSeekCloser struct {
	*bytes.Reader
}

func (*bytesReadSeekCloser) Close() error { return nil }
