package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// With its own pool settings the fleet plane runs the heartbeat on a pool
// whose connections carry application_name groundcontrol-fleet and the
// configured statement_timeout, while operator routes stay on the API pool.
func TestFleetPlaneUsesItsOwnPool(t *testing.T) {
	t.Setenv("DB_FLEET_MAX_OPEN_CONNS", "2")
	t.Setenv("DB_FLEET_STATEMENT_TIMEOUT", "3s")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")

	require.NotSame(t, h.app.db, h.app.pools.Fleet)
	require.Same(t, h.app.db, h.app.pools.Async)
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, creds.Password), http.StatusOK)

	var timeout string
	require.NoError(t, h.app.pools.Fleet.QueryRowContext(context.Background(), "SHOW statement_timeout").Scan(&timeout))
	require.Equal(t, "3s", timeout)
	var fleetConns int
	require.NoError(t, h.db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND application_name = 'groundcontrol-fleet'").Scan(&fleetConns))
	require.Positive(t, fleetConns)
	require.Equal(t, 2, h.app.pools.Fleet.Stats().MaxOpenConnections)
}
