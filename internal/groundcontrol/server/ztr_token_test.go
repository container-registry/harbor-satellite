package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/stretchr/testify/require"
)

func TestZtr_FailureAfterClaimUnclaimsToken(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	token := h.registerSatellite("edge-01", "edge-config", "edge-group")
	h.harbor.FailOn(harbortest.RouteRefreshRobot, http.StatusInternalServerError)

	h.mustDo(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil, http.StatusInternalServerError)
	h.harbor.ClearFaults()

	h.ztr(token)
}

func TestZtr_ConsumedTokenIsRejectedAsInvalid(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	token := h.registerSatellite("edge-01", "edge-config", "edge-group")
	_, err := h.db.ExecContext(context.Background(), "UPDATE satellite_token SET consumed_at = NOW() WHERE token = $1", token)
	require.NoError(t, err)

	r := h.mustDo(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil, http.StatusBadRequest)

	var body AppError
	r.decode(t, &body)
	require.Equal(t, "Error: Invalid Token", body.Message)
}

func TestZtr_ExpiredTokenStill401(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	token := h.registerSatellite("edge-01", "edge-config", "edge-group")
	// A day back, so the time zones of GC and the database cannot matter.
	_, err := h.db.ExecContext(context.Background(), "UPDATE satellite_token SET expires_at = NOW() - INTERVAL '1 day' WHERE token = $1", token)
	require.NoError(t, err)

	h.mustDo(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil, http.StatusUnauthorized)
}
