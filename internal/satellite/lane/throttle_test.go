package lane_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/lane"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestThrottledTransport_StaysUnderCap(t *testing.T) {
	pending.Plan(t, "6.2", "replication has no bandwidth budget")
	const capBytes = 64 << 10
	body := bytes.Repeat([]byte("x"), 2*capBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write(body)
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: lane.ThrottledTransport(http.DefaultTransport, capBytes)}

	start := time.Now()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	elapsed := time.Since(start)

	require.Len(t, got, len(body))
	// One second may burst; the second 64 KiB must wait for the budget.
	require.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "128 KiB at 64 KiB/s took %s", elapsed)
}

// A per-request bucket would let parallel requests multiply the cap.
func TestThrottledTransport_CapIsSharedAcrossRequests(t *testing.T) {
	pending.Plan(t, "6.2", "replication has no bandwidth budget")
	const capBytes = 64 << 10
	body := bytes.Repeat([]byte("x"), capBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: lane.ThrottledTransport(http.DefaultTransport, capBytes)}
	fetch := func() error {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return errors.Join(err, resp.Body.Close())
		}
		return resp.Body.Close()
	}

	start := time.Now()
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- fetch() }()
	}
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "2 x 64 KiB at 64 KiB/s took %s", elapsed)
}

func TestThrottledTransport_ZeroIsUncapped(t *testing.T) {
	base := http.DefaultTransport
	require.Equal(t, base, lane.ThrottledTransport(base, 0))
	require.Equal(t, base, lane.ThrottledTransport(base, -1))
}
