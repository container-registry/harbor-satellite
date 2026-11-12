package telemetry_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, opts telemetry.ServerOptions) (*telemetry.Server, string) {
	t.Helper()
	srv, err := telemetry.NewServer(opts)
	require.NoError(t, err)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after the context was cancelled")
		}
	})
	return srv, "http://" + l.Addr().String()
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func TestServer_HealthzAndMetrics(t *testing.T) {
	pending.Plan(t, "0.4", "the satellite has no /healthz and no /metrics")
	srv, base := serve(t, telemetry.ServerOptions{
		Checks: map[string]telemetry.HealthCheck{
			"store": func(context.Context) error { return nil },
		},
	})
	srv.Recorder().ObservePull("manifest", "hit", 20*time.Millisecond)

	status, _ := get(t, base+"/healthz")
	require.Equal(t, http.StatusOK, status)

	status, body := get(t, base+"/metrics")
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, telemetry.MetricPullDuration)
}

func TestServer_HealthzFailsWhenCheckFails(t *testing.T) {
	pending.Plan(t, "0.4", "the satellite has no /healthz and no /metrics")
	_, base := serve(t, telemetry.ServerOptions{
		Checks: map[string]telemetry.HealthCheck{
			"store": func(context.Context) error { return nil },
			"state": func(context.Context) error { return errors.New("state.json unreadable") },
		},
	})

	status, body := get(t, base+"/healthz")
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Contains(t, body, "state")
}
