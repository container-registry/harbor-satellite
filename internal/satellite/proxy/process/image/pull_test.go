package image_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy/process/image"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/google/go-containerregistry/pkg/registry"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

type fakeStore struct {
	pullCalls      atomic.Int32
	fetchCalls     atomic.Int32
	replicateCalls atomic.Int32
	pull           func(context.Context, store.Artifact, store.PullResource, int32) (ocispec.Descriptor, error)
	fetch          func(context.Context, store.Artifact, ocispec.Descriptor) (io.ReadCloser, error)
	replicate      func(context.Context, store.Store, []store.Artifact) error
}

func (f *fakeStore) Pull(ctx context.Context, artifact store.Artifact, resource store.PullResource) (ocispec.Descriptor, error) {
	call := f.pullCalls.Add(1)
	return f.pull(ctx, artifact, resource, call)
}

func (f *fakeStore) Fetch(ctx context.Context, artifact store.Artifact, descriptor ocispec.Descriptor) (io.ReadCloser, error) {
	f.fetchCalls.Add(1)
	return f.fetch(ctx, artifact, descriptor)
}

func (f *fakeStore) Replicate(ctx context.Context, source store.Store, artifacts []store.Artifact) error {
	f.replicateCalls.Add(1)
	return f.replicate(ctx, source, artifacts)
}

func (*fakeStore) Delete(context.Context, []store.Artifact) error { return nil }

func TestPullCoalescesCacheFillAndOpensIndependentStreams(t *testing.T) {
	payload := []byte(`{"schemaVersion":2}`)
	descriptor := descriptorFor(ocispec.MediaTypeImageManifest, payload)
	releaseFill := make(chan struct{})
	var available atomic.Bool
	local := &fakeStore{
		pull: func(_ context.Context, _ store.Artifact, _ store.PullResource, _ int32) (ocispec.Descriptor, error) {
			if !available.Load() {
				return ocispec.Descriptor{}, errdef.ErrNotFound
			}
			return descriptor, nil
		},
		fetch: func(context.Context, store.Artifact, ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(payload)), nil
		},
		replicate: func(context.Context, store.Store, []store.Artifact) error {
			<-releaseFill
			available.Store(true)
			return nil
		},
	}
	remote := &fakeStore{
		pull: func(context.Context, store.Artifact, store.PullResource, int32) (ocispec.Descriptor, error) {
			return descriptor, nil
		},
		fetch:     func(context.Context, store.Artifact, ocispec.Descriptor) (io.ReadCloser, error) { return nil, nil },
		replicate: func(context.Context, store.Store, []store.Artifact) error { return nil },
	}
	handler := proxy.New(image.NewPull(context.Background(), proxy.ModeProxy, local, remote)).Handler()

	const requestCount = 16
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, requestCount)
	for range requestCount {
		go func() {
			<-start
			responses <- serve(handler, http.MethodGet, "/v2/team/app/manifests/latest", nil)
		}()
	}
	close(start)
	require.Eventually(t, func() bool { return local.replicateCalls.Load() == 1 }, time.Second, time.Millisecond)
	close(releaseFill)
	for range requestCount {
		response := <-responses
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, payload, response.Body.Bytes())
	}
	require.GreaterOrEqual(t, local.pullCalls.Load(), int32(2))
	require.Equal(t, int32(0), remote.pullCalls.Load())
	require.Equal(t, int32(1), local.replicateCalls.Load())
	require.Equal(t, int32(requestCount), local.fetchCalls.Load())
}

func TestReplicaModeDoesNotContactUpstreamOnLocalMiss(t *testing.T) {
	descriptor := descriptorFor(ocispec.MediaTypeImageManifest, []byte(`{"schemaVersion":2}`))
	local := &fakeStore{
		pull: func(context.Context, store.Artifact, store.PullResource, int32) (ocispec.Descriptor, error) {
			return ocispec.Descriptor{}, errdef.ErrNotFound
		},
		fetch:     func(context.Context, store.Artifact, ocispec.Descriptor) (io.ReadCloser, error) { return nil, nil },
		replicate: func(context.Context, store.Store, []store.Artifact) error { return nil },
	}
	remote := descriptorStore(descriptor, []byte(`{"schemaVersion":2}`))
	handler := proxy.New(image.NewPull(context.Background(), proxy.ModeReplica, local, remote)).Handler()

	response := serve(handler, http.MethodGet, "/v2/team/app/manifests/latest", nil)

	require.Equal(t, http.StatusNotFound, response.Code)
	require.Contains(t, response.Body.String(), string(proxy.ErrorCodeManifestUnknown))
	require.Equal(t, int32(0), local.replicateCalls.Load())
	require.Equal(t, int32(0), remote.pullCalls.Load())
	require.Equal(t, int32(0), remote.fetchCalls.Load())
}

func TestCancelledWaiterDoesNotCancelSharedFill(t *testing.T) {
	payload := []byte("shared content")
	descriptor := descriptorFor("application/octet-stream", payload)
	fillStarted := make(chan struct{})
	releaseFill := make(chan struct{})
	var once sync.Once
	var available atomic.Bool
	local := &fakeStore{
		pull: func(context.Context, store.Artifact, store.PullResource, int32) (ocispec.Descriptor, error) {
			if available.Load() {
				return descriptor, nil
			}
			return ocispec.Descriptor{}, errdef.ErrNotFound
		},
		fetch: func(context.Context, store.Artifact, ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(payload)), nil
		},
		replicate: func(ctx context.Context, _ store.Store, _ []store.Artifact) error {
			once.Do(func() { close(fillStarted) })
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-releaseFill:
				available.Store(true)
				return nil
			}
		},
	}
	remote := descriptorStore(descriptor, payload)
	handler := proxy.New(image.NewPull(context.Background(), proxy.ModeProxy, local, remote)).Handler()
	firstContext, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v2/team/app/blobs/"+descriptor.Digest.String(), nil).WithContext(firstContext)
		handler.ServeHTTP(response, request)
		firstDone <- response
	}()
	<-fillStarted
	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		secondDone <- serve(handler, http.MethodGet, "/v2/team/app/blobs/"+descriptor.Digest.String(), nil)
	}()
	time.Sleep(20 * time.Millisecond)
	cancelFirst()
	require.Equal(t, http.StatusInternalServerError, (<-firstDone).Code)
	close(releaseFill)
	second := <-secondDone
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, payload, second.Body.Bytes())
	require.Equal(t, int32(1), local.replicateCalls.Load())
}

func TestPullSupportsRangeConditionalAndHeadRequests(t *testing.T) {
	payload := []byte("0123456789")
	descriptor := descriptorFor("application/octet-stream", payload)
	local := descriptorStore(descriptor, payload)
	handler := proxy.New(image.NewPull(context.Background(), proxy.ModeReplica, local, nil)).Handler()
	path := "/v2/team/app/blobs/" + descriptor.Digest.String()
	ranged := serve(handler, http.MethodGet, path, http.Header{"Range": []string{"bytes=2-5"}})
	require.Equal(t, http.StatusPartialContent, ranged.Code)
	require.Equal(t, "2345", ranged.Body.String())
	notModified := serve(handler, http.MethodGet, path, http.Header{"If-None-Match": []string{`"` + descriptor.Digest.String() + `"`}})
	require.Equal(t, http.StatusNotModified, notModified.Code)
	head := serve(handler, http.MethodHead, path, nil)
	require.Equal(t, http.StatusOK, head.Code)
	require.Empty(t, head.Body.Bytes())
}

func TestPullStreamsBeforeSourceEOF(t *testing.T) {
	prefix := []byte("first-")
	suffix := []byte("second")
	payload := append(append([]byte(nil), prefix...), suffix...)
	descriptor := descriptorFor("application/octet-stream", payload)
	releaseEOF := make(chan struct{})
	reader := &gatedReadCloser{prefix: prefix, suffix: suffix, release: releaseEOF}
	local := descriptorStore(descriptor, payload)
	local.fetch = func(context.Context, store.Artifact, ocispec.Descriptor) (io.ReadCloser, error) {
		return reader, nil
	}
	handler := proxy.New(image.NewPull(context.Background(), proxy.ModeProxy, local, descriptorStore(descriptor, payload))).Handler()
	response := newObservingResponseWriter()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/team/app/blobs/"+descriptor.Digest.String(), nil))
		close(done)
	}()
	select {
	case <-response.firstWrite:
		require.Equal(t, prefix, response.bytes())
	case <-time.After(time.Second):
		t.Fatal("response did not stream before source EOF")
	}
	close(releaseEOF)
	<-done
	require.Equal(t, payload, response.bytes())
	require.True(t, reader.closed.Load())
}

func TestProxyPullsIntoOCIStoreAndServesOffline(t *testing.T) {
	upstream := httptest.NewServer(registry.New())
	address := strings.TrimPrefix(upstream.URL, "http://")
	repository, err := orasremote.NewRepository(address + "/team/app")
	require.NoError(t, err)
	repository.PlainHTTP = true
	layer := []byte("satellite-streamed-layer")
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", layer)
	configPayload := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
	manifestPayload, err := json.Marshal(ocispec.Manifest{Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest, Config: configDesc, Layers: []ocispec.Descriptor{layerDesc}})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	require.NoError(t, repository.Push(context.Background(), layerDesc, bytes.NewReader(layer)))
	require.NoError(t, repository.Push(context.Background(), configDesc, bytes.NewReader(configPayload)))
	require.NoError(t, repository.PushReference(context.Background(), manifestDesc, bytes.NewReader(manifestPayload), "latest"))
	local, err := store.NewOCIStore(t.TempDir())
	require.NoError(t, err)
	remote, err := store.NewRegistryStore(store.RegistryOptions{Endpoint: address, PlainHTTP: true})
	require.NoError(t, err)
	handler := proxy.New(image.NewPull(context.Background(), proxy.ModeProxy, local, remote)).Handler()
	manifest := serve(handler, http.MethodGet, "/v2/team/app/manifests/latest", nil)
	require.Equal(t, http.StatusOK, manifest.Code)
	require.Equal(t, manifestPayload, manifest.Body.Bytes())
	upstream.Close()
	offlineManifest := serve(handler, http.MethodGet, "/v2/team/app/manifests/latest", nil)
	require.Equal(t, http.StatusOK, offlineManifest.Code)
	require.Equal(t, manifestPayload, offlineManifest.Body.Bytes())
	offlineBlob := serve(handler, http.MethodGet, "/v2/team/app/blobs/"+layerDesc.Digest.String(), nil)
	require.Equal(t, http.StatusOK, offlineBlob.Code)
	require.Equal(t, layer, offlineBlob.Body.Bytes())
}

func TestRegistryCheckDoesNotRequireStores(t *testing.T) {
	handler := proxy.New(image.NewPull(context.Background(), proxy.ModeReplica, nil, nil)).Handler()
	response := serve(handler, http.MethodGet, "/v2/", nil)
	require.Equal(t, http.StatusOK, response.Code)
}

func descriptorStore(descriptor ocispec.Descriptor, payload []byte) *fakeStore {
	return &fakeStore{
		pull: func(context.Context, store.Artifact, store.PullResource, int32) (ocispec.Descriptor, error) {
			return descriptor, nil
		},
		fetch: func(context.Context, store.Artifact, ocispec.Descriptor) (io.ReadCloser, error) {
			return &readSeekCloser{Reader: bytes.NewReader(payload)}, nil
		},
		replicate: func(context.Context, store.Store, []store.Artifact) error { return nil },
	}
}

func descriptorFor(mediaType string, payload []byte) ocispec.Descriptor {
	return ocispec.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(payload), Size: int64(len(payload))}
}

func serve(handler http.Handler, method, path string, header http.Header) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.Header = header.Clone()
	handler.ServeHTTP(response, request)
	return response
}

type readSeekCloser struct{ *bytes.Reader }

func (*readSeekCloser) Close() error { return nil }

type gatedReadCloser struct {
	prefix  []byte
	suffix  []byte
	release <-chan struct{}
	step    int
	closed  atomic.Bool
}

func (reader *gatedReadCloser) Read(destination []byte) (int, error) {
	switch reader.step {
	case 0:
		reader.step++
		return copy(destination, reader.prefix), nil
	case 1:
		<-reader.release
		reader.step++
		return copy(destination, reader.suffix), nil
	default:
		return 0, io.EOF
	}
}

func (reader *gatedReadCloser) Close() error {
	reader.closed.Store(true)
	return nil
}

type observingResponseWriter struct {
	header     http.Header
	mu         sync.Mutex
	body       bytes.Buffer
	firstWrite chan struct{}
	once       sync.Once
}

func newObservingResponseWriter() *observingResponseWriter {
	return &observingResponseWriter{header: make(http.Header), firstWrite: make(chan struct{})}
}

func (writer *observingResponseWriter) Header() http.Header { return writer.header }
func (*observingResponseWriter) WriteHeader(int)            {}
func (writer *observingResponseWriter) Write(payload []byte) (int, error) {
	writer.mu.Lock()
	written, err := writer.body.Write(payload)
	writer.mu.Unlock()
	writer.once.Do(func() { close(writer.firstWrite) })
	return written, err
}

func (writer *observingResponseWriter) bytes() []byte {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return append([]byte(nil), writer.body.Bytes()...)
}

var _ store.Store = (*fakeStore)(nil)
