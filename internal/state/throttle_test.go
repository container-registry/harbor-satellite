package state

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestThrottledTransport_PassesThrough(t *testing.T) {
	want := []byte("hello throttled world")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(want)
	}))
	t.Cleanup(srv.Close)

	// 100 Mbps — effectively unthrottled for a small payload
	tr := newThrottledTransport(http.DefaultTransport, 100)
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestThrottledTransport_LimitsRate(t *testing.T) {
	// 1 Mbps = 125,000 bytes/sec. 250 KB should take at least 1s.
	payload := bytes.Repeat([]byte("x"), 250_000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	tr := newThrottledTransport(http.DefaultTransport, 1)
	client := &http.Client{Transport: tr}

	start := time.Now()
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)

	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, time.Second, "transfer should be throttled to ~1 Mbps")
}

func TestThrottledReader_ContextCancellation(t *testing.T) {
	payload := bytes.Repeat([]byte("y"), 1_000_000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	// Very low limit so token-bucket waits long enough for the deadline to fire.
	tr := newThrottledTransport(http.DefaultTransport, 0.01)
	client := &http.Client{Transport: tr}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	require.Error(t, err, "context cancellation should surface as a read error")
}

func TestNewBasicReplicatorWithTLS_ThrottledReplication(t *testing.T) {
	_, srcAddr := newTestRegistry(t)
	_, dstAddr := newTestRegistry(t)

	pushImage(t, srcAddr, "library", "busybox", "latest", 1)

	r := NewBasicReplicatorWithTLS("", "", srcAddr, dstAddr, "", "", true,
		ReplicatorOptions{SyncConfig: config.SyncConfig{MaxBandwidthMbps: 100}},
	)

	ctx := testContext()
	err := r.Replicate(ctx, []Entity{{Name: "busybox", Repository: "library", Tag: "latest"}})
	require.NoError(t, err)
}
