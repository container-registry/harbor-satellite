package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/singleflight"
	"oras.land/oras-go/v2/errdef"
)

const distributionAPIVersion = "registry/2.0"

type pullProcess struct {
	lifecycleCtx context.Context
	mode         proxy.Mode
	localStore   store.Store
	remoteStore  store.Store
	operations   singleflight.Group
}

// NewPull serves retained content from localStore and fills it from remoteStore
// on a miss. The shared operation returns only an immutable descriptor; every
// HTTP request opens and streams its own reader after the flight completes.
func NewPull(lifecycleCtx context.Context, mode proxy.Mode, localStore, remoteStore store.Store) proxy.Process {
	if lifecycleCtx == nil {
		lifecycleCtx = context.Background()
	}
	process := &pullProcess{
		lifecycleCtx: lifecycleCtx,
		mode:         mode,
		localStore:   localStore,
		remoteStore:  remoteStore,
	}
	return process.execute
}

func (p *pullProcess) execute(request *proxy.Request) error {
	request.ResponseHeader().Set("Docker-Distribution-API-Version", distributionAPIVersion)
	if request.Operation == proxy.CheckRegistry {
		return request.Write(http.StatusOK, nil)
	}
	log.Printf("pulling %s %s:%s", request.Operation, request.Repository, request.Reference)

	artifact, resource, identifier, err := pullArtifactFor(request)
	if err != nil {
		return err
	}
	if !p.mode.Valid() {
		return errors.New("proxy image pull mode is invalid")
	}
	if p.localStore == nil {
		return errors.New("proxy image pull requires a local store")
	}
	if p.mode.AllowsUpstreamPull() && p.remoteStore == nil {
		return errors.New("proxy mode requires a remote store")
	}
	if err := request.Context().Err(); err != nil {
		return err
	}

	resultChannel := p.operations.DoChan(pullKey(resource, artifact.Name, identifier), func() (any, error) {
		if err := p.lifecycleCtx.Err(); err != nil {
			return nil, err
		}
		descriptor, err := p.localStore.Pull(p.lifecycleCtx, artifact, resource)
		if err == nil {
			return descriptor, nil
		}
		if !errors.Is(err, errdef.ErrNotFound) {
			return nil, err
		}
		if !p.mode.AllowsUpstreamPull() {
			return nil, err
		}

		if err := p.localStore.Replicate(p.lifecycleCtx, p.remoteStore, []store.Artifact{artifact}); err != nil {
			return nil, err
		}
		return p.localStore.Pull(p.lifecycleCtx, artifact, resource)
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

	body, err := p.localStore.Fetch(request.Context(), artifact, descriptor)
	if err != nil {
		return mapPullError(resource, err)
	}
	if body == nil {
		return errors.New("proxy image pull returned a nil content reader")
	}
	return writePullResponse(request, resource, descriptor, body)
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
			fmt.Sprintf("operation %s is not supported by the image pull process", request.Operation),
			nil,
		)
	}
	return store.Artifact{}, 0, "", errors.New("proxy image pull operation is invalid")
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

func writePullResponse(request *proxy.Request, resource store.PullResource, descriptor ocispec.Descriptor, body io.ReadCloser) error {
	header := make(http.Header)
	mediaType := descriptor.MediaType
	if mediaType == "" && resource == store.PullResourceBlob {
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

	return request.WriteContent(&http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          body,
		ContentLength: descriptor.Size,
	})
}
