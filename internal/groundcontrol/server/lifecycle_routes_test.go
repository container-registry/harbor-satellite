package server

import (
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingLifecycle = "ADR-0006 lifecycle is not implemented; satellites are never revoked"

func TestLifecycleRoutes_ServedAndGated(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	h.registerSatellite("edge-01", "edge-config", "edge-group")

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/satellites/edge-01/revoke"},
		{http.MethodPost, "/api/satellites/edge-01/unrevoke"},
		{http.MethodGet, "/api/satellites/edge-01/lifecycle"},
	} {
		require.Equal(t, http.StatusUnauthorized, h.do(route.method, route.path, nil, nil).Status, route.path)
		got := h.do(route.method, route.path, nil, h.admin).Status
		require.NotContains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnauthorized, http.StatusForbidden}, got, route.path)
	}
}

func TestRevoke_SyncAndZTRReturn403UntilUnrevoked(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	robot := basicAuth(creds.Username, creds.Password)
	token := h.registerSatellite("edge-02", "edge-config", "edge-group")

	var view SatelliteLifecycleResponse
	h.mustDo(http.MethodPost, "/api/satellites/edge-01/revoke", nil, h.admin, http.StatusOK).decode(t, &view)
	require.Equal(t, "revoked", view.State)
	h.mustDo(http.MethodPost, "/api/satellites/edge-02/revoke", nil, h.admin, http.StatusOK)

	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), robot, http.StatusForbidden)
	h.mustDo(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil, http.StatusForbidden)

	h.mustDo(http.MethodPost, "/api/satellites/edge-01/unrevoke", nil, h.admin, http.StatusOK).decode(t, &view)
	require.Equal(t, "pending", view.State)
	h.mustDo(http.MethodPost, "/api/satellites/edge-02/unrevoke", nil, h.admin, http.StatusOK)
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), robot, http.StatusOK)
	h.ztr(token)
}

func TestListActiveSatellites_ReadsViewInOneStatement(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, creds.Password), http.StatusOK)
	h.flushHeartbeats()

	used := h.db.MeasureStatements(t, func() {
		h.mustDo(http.MethodGet, "/api/satellites/active", nil, h.admin, http.StatusOK)
	})

	require.EqualValues(t, 1, used.Calls["ListSatelliteViewsByState"], used.String())
	require.Zero(t, used.Calls["GetActiveSatellites"], used.String())
}
