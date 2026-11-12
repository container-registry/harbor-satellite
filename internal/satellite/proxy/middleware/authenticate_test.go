package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy/middleware"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingEdge = "the proxy runs without middleware; it listens on loopback only"

type denyAll struct{ calls int }

func (d *denyAll) Verify(context.Context, *http.Request) (middleware.Peer, error) {
	d.calls++
	return middleware.Peer{}, middleware.ErrUnauthenticated
}

func serve(t *testing.T, mw proxy.MiddlewareFunc, remoteAddr string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	reached := false
	handler := proxy.New(func(r *proxy.Request) error {
		reached = true
		return r.Write(http.StatusOK, nil)
	}).Wrap(mw).Handler()
	req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/3.20", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, reached
}

func TestAuthenticate_RejectsUnauthenticatedPeer(t *testing.T) {
	pending.Plan(t, "9.3", pendingEdge)
	v := &denyAll{}

	rec, reached := serve(t, middleware.Authenticate(v), "10.0.0.5:4242")

	require.False(t, reached)
	require.Equal(t, 1, v.calls)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var body struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "UNAUTHORIZED", body.Errors[0].Code)
}

func TestAuthenticate_AdmitsLoopback(t *testing.T) {
	pending.Plan(t, "9.3", pendingEdge)
	v := &denyAll{}
	for _, addr := range []string{"127.0.0.1:4242", "[::1]:4242"} {
		_, reached := serve(t, middleware.Authenticate(v), addr)
		require.True(t, reached, addr)
	}
	require.Zero(t, v.calls, "loopback needs no verifier")
}

type recorder struct {
	pulls []string
}

func (r *recorder) ObservePull(resource, outcome string, _ time.Duration) {
	r.pulls = append(r.pulls, resource+":"+outcome)
}
func (r *recorder) CountPullThroughFill(string)                     {}
func (r *recorder) ObserveReplication(int64, time.Duration, string) {}
func (r *recorder) ObserveStoreLockWait(string, time.Duration)      {}

func TestMetrics_RecordsPullOutcome(t *testing.T) {
	pending.Plan(t, "9.3", pendingEdge)
	rec := &recorder{}

	_, reached := serve(t, middleware.Metrics(rec), "127.0.0.1:4242")

	require.True(t, reached)
	require.Len(t, rec.pulls, 1)
	require.Contains(t, rec.pulls[0], "manifest")
}
