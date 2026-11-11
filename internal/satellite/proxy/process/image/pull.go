package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/ready"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/singleflight"
	"oras.land/oras-go/v2/errdef"
)

const (
	distributionAPIVersion = "registry/2.0"
	pullFillTimeout        = 30 * time.Minute
)

type pullHandler struct {
	lifecycleCtx context.Context
	mode         proxy.Mode
	localStore   store.Store
	remoteStore  store.Store
	images       *ready.Gate
	operations   singleflight.Group
	filler       store.Filler
	prefetcher   Prefetcher
	recorder     telemetry.Recorder
	journal      PullThroughJournal
}

// NewPull serves retained content from localStore and fills it from remoteStore
// on a miss. images, when set, holds manifest requests for references the state
// job is publishing until that publish commits a complete local graph. The
// shared operation returns only an immutable descriptor; every HTTP request
// opens and streams its own reader after the flight completes.
func NewPull(lifecycleCtx context.Context, mode proxy.Mode, localStore, remoteStore store.Store, images *ready.Gate) (proxy.HandlerFunc, error) {
	return NewPullWithOptions(lifecycleCtx, mode, localStore, remoteStore, PullOptions{Gate: images})
}

func (p *pullHandler) handle(request *proxy.Request) (err error) {
	request.ResponseHeader().Set("Docker-Distribution-API-Version", distributionAPIVersion)
	if request.Operation == proxy.CheckRegistry {
		return request.Write(http.StatusOK, nil)
	}

	artifact, resource, identifier, err := pullArtifactFor(request)
	if err != nil {
		return err
	}
	started := time.Now()
	defer func() {
		p.recorder.ObservePull(resourceLabel(resource), pullOutcome(err), time.Since(started))
	}()
	if !p.mode.Valid() {
		return errors.New("proxy image pull mode is invalid")
	}
	if p.localStore == nil {
		return errors.New("proxy image pull requires a local store")
	}
	if p.mode.AllowsUpstreamPull() && p.remoteStore == nil {
		return errors.New("proxy mode requires a remote store")
	}
	if err = request.Context().Err(); err != nil {
		return err
	}

	metadataOnly := request.Operation == proxy.CheckManifest || request.Operation == proxy.CheckBlob
	handled, err := p.serveIfWatched(request, artifact, resource, metadataOnly)
	if handled {
		return err
	}
	if p.filler != nil && p.mode.AllowsUpstreamPull() && !metadataOnly && !p.known(artifact) {
		return p.handleStreaming(request, artifact, resource, identifier)
	}
	key := pullKey(resource, artifact.Name, identifier)
	if metadataOnly {
		key += "\x00head"
	}
	resultChannel := p.operations.DoChan(key, func() (any, error) {
		fillCtx, cancel := context.WithTimeout(p.lifecycleCtx, pullFillTimeout)
		defer cancel()
		if ctxErr := fillCtx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		descriptor, fillErr := p.localDescriptor(fillCtx, artifact, resource)
		if fillErr == nil {
			return descriptor, nil
		}
		if !errors.Is(fillErr, errdef.ErrNotFound) {
			return nil, fillErr
		}
		// The reference may have entered the desired set after this flight
		// started. A desired image is served only from the local store.
		if resource == store.PullResourceManifest && p.watching(artifact) {
			return p.waitForLocal(fillCtx, artifact, resource)
		}
		if resource == store.PullResourceManifest && p.known(artifact) {
			return nil, errdef.ErrNotFound
		}
		if !p.mode.AllowsUpstreamPull() {
			return nil, fillErr
		}
		if metadataOnly {
			return p.remoteStore.Pull(fillCtx, artifact, resource)
		}

		started := time.Now()
		if fillErr = p.localStore.Replicate(fillCtx, p.remoteStore, []store.Artifact{artifact}); fillErr != nil {
			p.recorder.CountPullThroughFill("error")
			p.journalFill(resource, artifact, identifier, ocispec.Descriptor{}, started, fillErr)
			return nil, fillErr
		}
		p.recorder.CountPullThroughFill("ok")
		descriptor, fillErr = p.localDescriptor(fillCtx, artifact, resource)
		p.journalFill(resource, artifact, identifier, descriptor, started, fillErr)
		return descriptor, fillErr
	})

	var operationResult singleflight.Result
	select {
	case <-request.Context().Done():
		return request.Context().Err()
	case operationResult = <-resultChannel:
	}
	if operationResult.Err != nil {
		return mapPullError(resource, operationResult.Err)
	}
	descriptor, ok := operationResult.Val.(ocispec.Descriptor)
	if !ok {
		return errors.New("proxy image pull returned an invalid descriptor")
	}
	return p.writeResolved(request, artifact, resource, descriptor, metadataOnly)
}

// serveIfWatched blocks a manifest request for a desired image until state
// replication commits it, then answers from the local store only.
func (p *pullHandler) serveIfWatched(request *proxy.Request, artifact store.Artifact, resource store.PullResource, metadataOnly bool) (bool, error) {
	if resource != store.PullResourceManifest || !p.watching(artifact) {
		return false, nil
	}
	descriptor, err := p.localManifest(request.Context(), artifact)
	if err == nil {
		return true, p.writeResolved(request, artifact, resource, descriptor, metadataOnly)
	}
	if !errors.Is(err, errdef.ErrNotFound) {
		return true, err
	}
	waitCtx, cancel := p.waitContext(request.Context())
	defer cancel()
	committed, err := p.images.Wait(waitCtx, artifact)
	if err != nil {
		return true, err
	}
	if !committed {
		return false, nil
	}
	descriptor, err = p.localManifest(request.Context(), artifact)
	if err != nil {
		return true, mapPullError(resource, err)
	}
	return true, p.writeResolved(request, artifact, resource, descriptor, metadataOnly)
}

func (p *pullHandler) waitForLocal(ctx context.Context, artifact store.Artifact, resource store.PullResource) (ocispec.Descriptor, error) {
	committed, err := p.images.Wait(ctx, artifact)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if !committed {
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
	return p.lookupLocal(ctx, artifact, resource)
}

func (p *pullHandler) watching(artifact store.Artifact) bool {
	return p.images != nil && p.images.Watching(artifact)
}

func (p *pullHandler) known(artifact store.Artifact) bool {
	return p.images != nil && p.images.Known(artifact)
}

func (p *pullHandler) waitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	waitCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-p.lifecycleCtx.Done():
			cancel()
		case <-waitCtx.Done():
		}
	}()
	return waitCtx, cancel
}

// localDescriptor is the metadata lookup. A desired manifest is ready only
// when its whole graph is in the local store.
func (p *pullHandler) localDescriptor(ctx context.Context, artifact store.Artifact, resource store.PullResource) (ocispec.Descriptor, error) {
	if resource == store.PullResourceManifest && p.known(artifact) {
		return p.lookupLocal(ctx, artifact, resource)
	}
	return p.localStore.Pull(ctx, artifact, resource)
}

func (p *pullHandler) lookupLocal(ctx context.Context, artifact store.Artifact, resource store.PullResource) (ocispec.Descriptor, error) {
	if resource != store.PullResourceManifest {
		return p.localStore.Pull(ctx, artifact, resource)
	}
	return p.localManifest(ctx, artifact)
}

func (p *pullHandler) localManifest(ctx context.Context, artifact store.Artifact) (ocispec.Descriptor, error) {
	if p.images != nil {
		if digest, ok := p.images.ExpectedDigest(artifact); ok && artifact.Digest == "" {
			artifact.Digest = digest
		}
	}
	return store.ReadyManifest(ctx, p.localStore, artifact)
}

func (p *pullHandler) writeResolved(request *proxy.Request, artifact store.Artifact, resource store.PullResource, descriptor ocispec.Descriptor, metadataOnly bool) error {
	if metadataOnly {
		if descriptor.Size < 0 {
			setPullHeaders(request.ResponseHeader(), descriptor)
			return request.Write(http.StatusOK, nil)
		}
		return writePullResponse(request, descriptor, &metadataReadSeekCloser{
			SectionReader: io.NewSectionReader(metadataReaderAt{}, 0, descriptor.Size),
		})
	}

	response, err := p.localStore.Fetch(request.Context(), artifact, descriptor, resource, request.HTTPRequest().Header) //nolint:bodyclose // WriteContent owns and closes the content response body.
	if err != nil {
		return mapPullError(resource, err)
	}
	if response == nil || response.Body == nil {
		return errors.New("proxy image pull returned a nil content response")
	}
	return request.WriteContent(response)
}

func pullArtifactFor(request *proxy.Request) (
	artifact store.Artifact,
	resource store.PullResource,
	identifier string,
	err error,
) {
	artifact = store.Artifact{Name: request.Repository}
	switch request.Operation {
	case proxy.PullManifest, proxy.CheckManifest:
		artifact.Tag = request.Reference
		return artifact, store.PullResourceManifest, request.Reference, nil
	case proxy.PullBlob, proxy.CheckBlob:
		identifier = request.Digest.String()
		artifact.Digest = identifier
		return artifact, store.PullResourceBlob, identifier, nil
	case proxy.OperationUnknown,
		proxy.CheckRegistry,
		proxy.PushManifest,
		proxy.DeleteManifest,
		proxy.PushBlob,
		proxy.DeleteBlob,
		proxy.StartBlobUpload,
		proxy.CheckBlobUpload,
		proxy.UpdateBlobUpload,
		proxy.CompleteBlobUpload,
		proxy.MountBlob,
		proxy.ListTags,
		proxy.ListReferrers:
		return store.Artifact{}, 0, "", proxy.NewError(
			proxy.ErrorCodeUnsupported,
			fmt.Sprintf("operation %s is not supported by the image pull handler", request.Operation),
			nil,
		)
	}
	return store.Artifact{}, 0, "", errors.New("proxy image pull operation is invalid")
}

func resourceLabel(resource store.PullResource) string {
	if resource == store.PullResourceBlob {
		return "blob"
	}
	return "manifest"
}

func pullOutcome(err error) string {
	var distributionErr *proxy.DistributionError
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.As(err, &distributionErr) && (distributionErr.Code == proxy.ErrorCodeBlobUnknown || distributionErr.Code == proxy.ErrorCodeManifestUnknown):
		return "not_found"
	default:
		return "error"
	}
}

func pullKey(resource store.PullResource, repository, identifier string) string {
	return strings.Join([]string{strconv.Itoa(int(resource)), repository, identifier}, "\x00")
}

func mapPullError(resource store.PullResource, err error) error {
	if !errors.Is(err, errdef.ErrNotFound) {
		return err
	}
	if resource == store.PullResourceBlob {
		return proxy.NewError(proxy.ErrorCodeBlobUnknown, "blob is not available", nil)
	}
	return proxy.NewError(proxy.ErrorCodeManifestUnknown, "manifest is not available", nil)
}

func writePullResponse(request *proxy.Request, descriptor ocispec.Descriptor, body io.ReadCloser) error {
	header := make(http.Header)
	setPullHeaders(header, descriptor)

	return request.WriteContent(&http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          body,
		ContentLength: descriptor.Size,
	})
}

func setPullHeaders(header http.Header, descriptor ocispec.Descriptor) {
	mediaType := descriptor.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	if mediaType != "" {
		header.Set("Content-Type", mediaType)
	}
	if descriptor.Size >= 0 {
		header.Set("Content-Length", strconv.FormatInt(descriptor.Size, 10))
	}
	if descriptor.Digest != "" {
		digest := descriptor.Digest.String()
		header.Set("Docker-Content-Digest", digest)
		header.Set("ETag", strconv.Quote(digest))
	}
	header.Set("Docker-Distribution-API-Version", distributionAPIVersion)
}

// ServeContent only seeks a HEAD body to determine its length; it never reads
// bytes. This reader retains HEAD conditional and range behavior without
// opening content from either store.
type metadataReadSeekCloser struct {
	*io.SectionReader
}

func (*metadataReadSeekCloser) Close() error { return nil }

type metadataReaderAt struct{}

func (metadataReaderAt) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
