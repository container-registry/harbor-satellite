package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// rotateRobots runs one robot rotator pass and refreshes the server's
// RotationCache, so the next beat sees the rotation without waiting for
// ROBOT_ROTATION_CACHE_REFRESH.
func (h *gcHarness) rotateRobots() {
	h.t.Helper()
	require.NotNil(h.t, h.hot.rotator, "ROBOT_ROTATION_ENABLED composes the rotator")
	_, _, err := h.hot.rotator.RunOnce(context.Background())
	require.NoError(h.t, err)
	require.NoError(h.t, h.hot.rotations.Refresh(context.Background()))
}

func robotName(t *testing.T, h *gcHarness, satellite string) string {
	t.Helper()
	var name string
	require.NoError(t, h.db.QueryRowContext(t.Context(),
		"SELECT ra.robot_name FROM robot_accounts ra JOIN satellites s ON s.id = ra.satellite_id WHERE s.name = $1", satellite).Scan(&name))
	return name
}

// The old and the successor robot both exist and stay enabled until the
// satellite acks the successor's generation; then the old one goes.
func TestSync_RotationOverlapWithoutDowntime(t *testing.T) {
	t.Setenv("ROBOT_ROTATION_ENABLED", "true")
	t.Setenv("ROBOT_ROTATION_BEFORE_EXPIRY", "8760h")
	t.Setenv("DATA_ENCRYPTION_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, token := h.onboardWithSession("edge-01")
	old := robotName(t, h, "edge-01")

	h.rotateRobots()
	resp := h.beat(heartbeatRequest("edge-01"), bearerAuth(token))

	require.NotNil(t, resp.Credentials, "the successor is delivered with the beat")
	successor, ok := h.harbor.Robot(resp.Credentials.Username)
	require.True(t, ok)
	require.False(t, successor.Disable)
	previous, ok := h.harbor.Robot(old)
	require.True(t, ok, "the old robot stays until the ack")
	require.False(t, previous.Disable)

	ack := heartbeatRequest("edge-01")
	ack.CredentialsGeneration = resp.Credentials.Generation
	h.beat(ack, bearerAuth(token))
	h.flushHeartbeats()
	h.rotateRobots()

	_, ok = h.harbor.Robot(old)
	require.False(t, ok, "promotion deletes the old robot")
	require.Equal(t, resp.Credentials.Username, robotName(t, h, "edge-01"))
}

func TestBudgetHeartbeat_RotationDeliveryAddsNoStatement(t *testing.T) {
	splitLimits(t, "100/1m", "100/1s")
	t.Setenv("ROBOT_ROTATION_ENABLED", "true")
	t.Setenv("ROBOT_ROTATION_BEFORE_EXPIRY", "8760h")
	t.Setenv("DATA_ENCRYPTION_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, rotating := h.onboardWithSession("edge-02")
	h.beat(heartbeatRequest("edge-02"), bearerAuth(rotating))
	h.rotateRobots()
	_, plain := h.onboardWithSession("edge-01")
	h.beat(heartbeatRequest("edge-01"), bearerAuth(plain))

	var plainResp, resp SatelliteSyncResponse
	without := h.measure(func() { plainResp = h.beat(heartbeatRequest("edge-01"), bearerAuth(plain)) })
	with := h.measure(func() { resp = h.beat(heartbeatRequest("edge-02"), bearerAuth(rotating)) })

	require.Nil(t, plainResp.Credentials, "edge-01 joined after the pass, so the baseline delivers nothing")
	require.NotNil(t, resp.Credentials)
	require.Equal(t, without.statements.Total, with.statements.Total, "delivery reads the replica's cache\n%s", with)
}
