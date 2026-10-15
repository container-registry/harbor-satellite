package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	groundcontrolmiddleware "github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/stretchr/testify/require"
)

func TestRefreshRoute_RequiresSatelliteAuth(t *testing.T) {
	rl := groundcontrolmiddleware.NewRateLimiter(10, time.Minute)
	t.Cleanup(rl.Stop)
	server := &Server{rateLimiter: rl}
	rec := httptest.NewRecorder()

	server.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/satellites/refresh", nil))

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func TestRefreshSatelliteCredentials_ExpiredRobotIs401(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	_, err := h.db.ExecContext(context.Background(), "UPDATE robot_accounts SET robot_expiry = NOW() - INTERVAL '1 hour'")
	require.NoError(t, err)
	mark := h.harbor.Mark()

	h.mustDo(http.MethodPost, "/satellites/refresh", nil, basicAuth(creds.Username, creds.Password), http.StatusUnauthorized)

	require.Empty(t, h.harbor.CallsSince(mark), "an expired robot must not reach Harbor")
}

func TestNotImplemented_Answers501WithPlanItem(t *testing.T) {
	rec := httptest.NewRecorder()

	notImplemented(rec, "X0 example item")

	require.Equal(t, http.StatusNotImplemented, rec.Code)
	var body AppError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body.Message, "X0 example item")
}
