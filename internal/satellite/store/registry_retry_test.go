package store

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

func TestRegistryHTTPClient_TLSKeepsRetries(t *testing.T) {
	pending.Plan(t, "6.3", "a TLS setting replaces the retrying transport with a plain one")
	var requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	client, err := registryHTTPClient(config.TLSConfig{SkipVerify: true})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v2/", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 2, requests.Load(), "the 503 is retried")
}

// The replication source sets BandwidthBytesPerSec on Harbor options that
// may carry TLS settings: the cap must not lose them, nor the retries.
func TestRegistryStore_BandwidthCapKeepsTLSAndRetries(t *testing.T) {
	pending.Plan(t, "6.2", "RegistryOptions.BandwidthBytesPerSec is not read")
	ctx := context.Background()
	handler := registry.New()
	var blobGets atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/") && blobGets.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	address := strings.TrimPrefix(srv.URL, "https://")
	repo, err := orasremote.NewRepository(address + "/team/app")
	require.NoError(t, err)
	repo.Client = srv.Client()
	layer := bytes.Repeat([]byte("satellite"), (128<<10)/9)
	desc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", layer)
	require.NoError(t, repo.Push(ctx, desc, bytes.NewReader(layer)))
	s, err := NewRegistryStore(RegistryOptions{Endpoint: address, TLS: config.TLSConfig{SkipVerify: true}, BandwidthBytesPerSec: 64 << 10})
	require.NoError(t, err)

	start := time.Now()
	resp, err := s.Fetch(ctx, Artifact{Name: "team/app", Digest: desc.Digest.String()}, desc, PullResourceBlob, nil)
	require.NoError(t, err)
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	elapsed := time.Since(start)

	require.Equal(t, layer, got, "the 503 was retried over TLS")
	require.EqualValues(t, 2, blobGets.Load())
	require.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "128 KiB at 64 KiB/s took %s", elapsed)
}
