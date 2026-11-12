package store

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

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/google/go-containerregistry/pkg/registry"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

// Fitness target for the edge pull path: a replication in progress must not
// stall local reads of content the store already holds.
func TestOCIStoreServesRetainedContentDuringReplication(t *testing.T) {
	pending.Plan(t, "5.1", "edge store lock model")

	const (
		readBound     = 250 * time.Millisecond
		throttleChunk = 4 << 10
		throttleDelay = 50 * time.Millisecond
	)
	var throttled atomic.Bool
	blobStarted := make(chan struct{})
	var startOnce sync.Once
	upstream := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if throttled.Load() && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/") {
			startOnce.Do(func() { close(blobStarted) })
			upstream.ServeHTTP(&throttledResponseWriter{ResponseWriter: w, chunk: throttleChunk, delay: throttleDelay}, r)
			return
		}
		upstream.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")

	retainedLayer := []byte("layer already retained on the edge")
	pushFitnessImage(t, address, "team/retained", retainedLayer)
	pushFitnessImage(t, address, "team/large", bytes.Repeat([]byte("x"), 32*throttleChunk))
	remote, err := NewRegistryStore(RegistryOptions{Endpoint: address, PlainHTTP: true})
	require.NoError(t, err)
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	nop := zerolog.Nop()
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), logger.LoggerKey, &nop), time.Minute)
	defer cancel()
	retained := Artifact{Name: "team/retained", Tag: "v1"}
	require.NoError(t, local.Replicate(ctx, remote, []Artifact{retained}))

	throttled.Store(true)
	replicated := make(chan error, 1)
	go func() {
		replicated <- local.Replicate(ctx, remote, []Artifact{{Name: "team/large", Tag: "v1"}})
	}()
	select {
	case <-blobStarted:
	case <-ctx.Done():
		t.Fatal("replication never requested a blob")
	}

	start := time.Now()
	manifestDesc, err := local.Pull(ctx, retained, PullResourceManifest)
	require.NoError(t, err)
	var manifest ocispec.Manifest
	require.NoError(t, json.Unmarshal(fetchFitnessContent(ctx, t, local, retained, manifestDesc, PullResourceManifest), &manifest))
	require.NotEmpty(t, manifest.Layers)
	layer := Artifact{Name: retained.Name, Digest: manifest.Layers[0].Digest.String()}
	layerDesc, err := local.Pull(ctx, layer, PullResourceBlob)
	require.NoError(t, err)
	require.Equal(t, retainedLayer, fetchFitnessContent(ctx, t, local, layer, layerDesc, PullResourceBlob))
	elapsed := time.Since(start)

	require.NoError(t, <-replicated)
	require.Less(t, elapsed, readBound, "local Pull and Fetch of retained content took %s while Replicate copied from a throttled upstream", elapsed)
}

func fetchFitnessContent(ctx context.Context, t *testing.T, local *OCIStore, artifact Artifact, desc ocispec.Descriptor, resource PullResource) []byte {
	t.Helper()
	response, err := local.Fetch(ctx, artifact, desc, resource, nil)
	require.NoError(t, err)
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return payload
}

func pushFitnessImage(t *testing.T, address, repository string, layer []byte) {
	t.Helper()
	repo, err := orasremote.NewRepository(address + "/" + repository)
	require.NoError(t, err)
	repo.PlainHTTP = true
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", layer)
	configPayload := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
	manifestPayload, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
		Config: configDesc, Layers: []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	ctx := context.Background()
	require.NoError(t, repo.Push(ctx, layerDesc, bytes.NewReader(layer)))
	require.NoError(t, repo.Push(ctx, configDesc, bytes.NewReader(configPayload)))
	require.NoError(t, repo.PushReference(ctx, manifestDesc, bytes.NewReader(manifestPayload), "v1"))
}

// throttledResponseWriter delivers a body in fixed chunks with a pause after
// each one, standing in for a slow WAN link to Harbor.
type throttledResponseWriter struct {
	http.ResponseWriter
	chunk int
	delay time.Duration
}

func (w *throttledResponseWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := min(w.chunk, len(p))
		m, err := w.ResponseWriter.Write(p[:n])
		written += m
		if err != nil {
			return written, err
		}
		if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(w.delay)
		p = p[n:]
	}
	return written, nil
}
