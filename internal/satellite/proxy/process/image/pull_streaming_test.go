package image_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/lane"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy/process/image"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/google/go-containerregistry/pkg/registry"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

// streamingUpstream is a registry whose blob GETs for held digests wait for
// release, and which counts blob GETs per digest.
type streamingUpstream struct {
	address  string
	remote   *store.RegistryStore
	held     map[digest.Digest]bool
	corrupt  map[digest.Digest]bool
	release  chan struct{}
	mu       sync.Mutex
	blobGETs map[digest.Digest]int
	// releaseHeld lets held blob GETs through; safe to call more than once.
	releaseHeld func()
}

func newStreamingUpstream(t *testing.T) *streamingUpstream {
	t.Helper()
	u := &streamingUpstream{
		held: map[digest.Digest]bool{}, corrupt: map[digest.Digest]bool{},
		release: make(chan struct{}), blobGETs: map[digest.Digest]int{},
	}
	u.releaseHeld = sync.OnceFunc(func() { close(u.release) })
	next := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ref, isBlob := strings.Cut(r.URL.Path, "/blobs/")
		if r.Method != http.MethodGet || !isBlob {
			next.ServeHTTP(w, r)
			return
		}
		d := digest.Digest(ref)
		u.mu.Lock()
		u.blobGETs[d]++
		held, corrupt := u.held[d], u.corrupt[d]
		u.mu.Unlock()
		if held {
			select {
			case <-u.release:
			case <-r.Context().Done():
				return
			}
		}
		if corrupt {
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			body := bytes.ToUpper(rec.Body.Bytes())
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			if _, err := w.Write(body); err != nil {
				log.Printf("serve corrupted blob: %v", err)
			}
			return
		}
		next.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	// Registered after the server, so it runs before Close waits for held requests.
	t.Cleanup(u.releaseHeld)
	u.address = strings.TrimPrefix(server.URL, "http://")
	remote, err := store.NewRegistryStore(store.RegistryOptions{Endpoint: u.address, PlainHTTP: true})
	require.NoError(t, err)
	u.remote = remote
	return u
}

func (u *streamingUpstream) hold(d digest.Digest) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.held[d] = true
}

func (u *streamingUpstream) corruptBlob(d digest.Digest) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.corrupt[d] = true
}

func (u *streamingUpstream) gets(d digest.Digest) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.blobGETs[d]
}

// push publishes repository:v1 with one config and the given layers.
func (u *streamingUpstream) push(t *testing.T, repository string, layers ...[]byte) (manifestPayload []byte, layerDescs []ocispec.Descriptor) {
	t.Helper()
	ctx := context.Background()
	repo, err := orasremote.NewRepository(u.address + "/" + repository)
	require.NoError(t, err)
	repo.PlainHTTP = true
	configPayload := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
	require.NoError(t, repo.Push(ctx, configDesc, bytes.NewReader(configPayload)))
	for _, layer := range layers {
		desc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageLayer, layer)
		require.NoError(t, repo.Push(ctx, desc, bytes.NewReader(layer)))
		layerDescs = append(layerDescs, desc)
	}
	manifestPayload, err = json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
		Config: configDesc, Layers: layerDescs,
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	require.NoError(t, repo.PushReference(ctx, manifestDesc, bytes.NewReader(manifestPayload), "v1"))
	return manifestPayload, layerDescs
}

// streamingProxy serves a streaming pull-through proxy over HTTP; with
// prefetch it completes graphs in a lane.
func streamingProxy(t *testing.T, upstream *streamingUpstream, prefetch bool) (string, *store.OCIStore) {
	t.Helper()
	nop := zerolog.Nop()
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), logger.LoggerKey, &nop))
	t.Cleanup(cancel)
	local, err := store.NewOCIStore(t.TempDir())
	require.NoError(t, err)
	opts := image.PullOptions{Streaming: true}
	if prefetch {
		prefetchLane, err := lane.New("prefetch", lane.Options{Concurrency: 2, QueueSize: 16})
		require.NoError(t, err)
		t.Cleanup(func() { _ = prefetchLane.Close(context.Background()) })
		opts.Prefetcher, err = image.NewLanePrefetcher(prefetchLane, local, upstream.remote)
		require.NoError(t, err)
	}
	handler, err := image.NewPullWithOptions(ctx, proxy.ModeProxy, local, upstream.remote, opts)
	require.NoError(t, err)
	server := httptest.NewServer(proxy.New(handler).Handler())
	t.Cleanup(server.Close)
	return server.URL, local
}

// getThroughProxy may run outside the test goroutine, so it reports instead of failing.
func getThroughProxy(url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

func TestPullThrough_ManifestReturnsWhileLayerBlocked(t *testing.T) {
	pending.Plan(t, "5.3", "a manifest miss copies the whole graph before the first byte")
	upstream := newStreamingUpstream(t)
	held := []byte("layer the upstream holds back")
	upstream.hold(digest.FromBytes(held))
	manifest, _ := upstream.push(t, "team/app", held)
	base, _ := streamingProxy(t, upstream, true)

	type result struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		status, body, err := getThroughProxy(base + "/v2/team/app/manifests/v1")
		done <- result{status, body, err}
	}()
	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.Equal(t, http.StatusOK, r.status)
		require.Equal(t, manifest, r.body)
	case <-time.After(time.Second):
		t.Fatal("the manifest response waited for a layer")
	}
}

func TestPullThrough_BlobCommitsOnlyVerified(t *testing.T) {
	pending.Plan(t, "5.3", "blob misses are not streamed")
	upstream := newStreamingUpstream(t)
	layer := []byte("layer the upstream corrupts in transit")
	_, layers := upstream.push(t, "team/app", layer)
	upstream.corruptBlob(layers[0].Digest)
	base, local := streamingProxy(t, upstream, false)

	status, body, err := getThroughProxy(base + "/v2/team/app/blobs/" + layers[0].Digest.String())
	servedAsComplete := err == nil && status == http.StatusOK && bytes.Equal(body, bytes.ToUpper(layer))
	require.False(t, servedAsComplete, "the proxy passed corrupted content off as complete")
	_, err = local.Pull(context.Background(), store.Artifact{Name: "team/app", Digest: layers[0].Digest.String()}, store.PullResourceBlob)
	require.ErrorIs(t, err, errdef.ErrNotFound, "content that failed verification was committed")
}

func TestPullThrough_TagsOnlyCompleteGraph(t *testing.T) {
	pending.Plan(t, "5.3", "a manifest miss copies the whole graph before the first byte")
	upstream := newStreamingUpstream(t)
	held := []byte("layer the upstream holds back")
	upstream.hold(digest.FromBytes(held))
	_, layers := upstream.push(t, "team/app", held)
	base, local := streamingProxy(t, upstream, true)
	tagged := store.Artifact{Name: "team/app", Tag: "v1"}

	status, _, err := getThroughProxy(base + "/v2/team/app/manifests/v1")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	_, err = local.Pull(context.Background(), tagged, store.PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound, "the tag exists before its graph is complete")

	upstream.releaseHeld()
	require.Eventually(t, func() bool {
		_, err := local.Pull(context.Background(), tagged, store.PullResourceManifest)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	_, err = local.Pull(context.Background(), store.Artifact{Name: "team/app", Digest: layers[0].Digest.String()}, store.PullResourceBlob)
	require.NoError(t, err)
}

func TestPullThrough_DedupesFillsPerDigest(t *testing.T) {
	pending.Plan(t, "5.3", "fills are deduplicated per repository and reference, not per digest (pull.go:73)")
	upstream := newStreamingUpstream(t)
	shared := []byte("layer shared by two repositories")
	sharedDigest := digest.FromBytes(shared)
	upstream.push(t, "team/a", shared)
	upstream.push(t, "team/b", shared)
	upstream.hold(sharedDigest)
	base, _ := streamingProxy(t, upstream, false)

	var wg sync.WaitGroup
	var ok atomic.Int32
	for _, repository := range []string{"team/a", "team/b"} {
		wg.Go(func() {
			status, body, err := getThroughProxy(base + "/v2/" + repository + "/blobs/" + sharedDigest.String())
			if err == nil && status == http.StatusOK && bytes.Equal(body, shared) {
				ok.Add(1)
			}
		})
	}
	require.Eventually(t, func() bool { return upstream.gets(sharedDigest) > 0 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	upstream.releaseHeld()
	wg.Wait()

	require.EqualValues(t, 2, ok.Load())
	require.Equal(t, 1, upstream.gets(sharedDigest))
}

func TestNewPullWithOptions_ZeroOptionsIsNewPull(t *testing.T) {
	handler, err := image.NewPullWithOptions(context.Background(), proxy.ModeReplica, nil, nil, image.PullOptions{})
	require.NoError(t, err)
	response := serve(proxy.New(handler).Handler(), http.MethodGet, "/v2/", nil)
	require.Equal(t, http.StatusOK, response.Code)

	var lifecycleCtx context.Context
	_, err = image.NewPullWithOptions(lifecycleCtx, proxy.ModeReplica, nil, nil, image.PullOptions{})
	require.EqualError(t, err, "image.NewPull: lifecycle context is required")
}
