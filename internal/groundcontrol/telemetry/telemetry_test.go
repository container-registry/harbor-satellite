package telemetry_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/telemetry"
	"github.com/stretchr/testify/require"
)

func scrape(t *testing.T, srv *telemetry.Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func TestServer_ExposesRequestsHeartbeatsOutboxAndPools(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	srv, err := telemetry.NewServer(telemetry.Options{
		Pools:           []telemetry.NamedPool{{Name: "api", DB: db}, {Name: "fleet", DB: db}},
		HeartbeatBuffer: func() int { return 7 },
		Outbox: func(context.Context) (telemetry.OutboxCounts, error) {
			return telemetry.OutboxCounts{Pending: 3, Failed: 1, OldestPending: 90 * time.Second}, nil
		},
	})
	require.NoError(t, err)
	rec := srv.Recorder()
	rec.ObserveHTTP("/api/groups/{group}", http.MethodGet, http.StatusOK, 20*time.Millisecond)
	rec.CountHeartbeat()
	rec.CountHeartbeat()
	rec.ObserveHeartbeatFlush(telemetry.OutcomeError, time.Millisecond)

	body := scrape(t, srv)
	for _, want := range []string{
		`groundcontrol_http_requests_total{code="200",method="GET",route="/api/groups/{group}"} 1`,
		`groundcontrol_http_request_duration_seconds_count{method="GET",route="/api/groups/{group}"} 1`,
		`groundcontrol_heartbeats_total 2`,
		`groundcontrol_heartbeat_buffer_satellites 7`,
		`groundcontrol_heartbeat_flushes_total{outcome="error"} 1`,
		`groundcontrol_outbox_events{state="pending"} 3`,
		`groundcontrol_outbox_events{state="failed"} 1`,
		`groundcontrol_outbox_oldest_pending_age_seconds 90`,
		`groundcontrol_outbox_stats_up 1`,
		`go_sql_max_open_connections{db_name="api"}`,
		`go_goroutines`,
	} {
		require.Contains(t, body, want)
	}
	require.NotContains(t, body, `db_name="fleet"`, "a pool shared by two planes is reported once")
}

func TestServer_OutboxQueryFailureIsReportedNotFatal(t *testing.T) {
	srv, err := telemetry.NewServer(telemetry.Options{
		Outbox: func(context.Context) (telemetry.OutboxCounts, error) {
			return telemetry.OutboxCounts{}, errors.New("database down")
		},
	})
	require.NoError(t, err)

	body := scrape(t, srv)
	require.Contains(t, body, `groundcontrol_outbox_stats_up 0`)
	require.NotContains(t, body, `groundcontrol_outbox_events{`)
}

func TestServer_ServeAnswersMetricsUntilCanceled(t *testing.T) {
	srv, err := telemetry.NewServer(telemetry.Options{})
	require.NoError(t, err)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, l) }()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+l.Addr().String()+"/metrics", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), telemetry.MetricHeartbeats)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the context was canceled")
	}
}
