package server

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// newTokenDB returns queries on a fresh database holding one satellite with
// a token that is valid and one that expired an hour ago.
func newTokenDB(t *testing.T) (q *database.Queries, satelliteID int32, valid, expired string) {
	t.Helper()
	q = database.New(dbtest.New(t).DB)
	ctx := context.Background()
	sat, err := q.CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	valid, expired = "valid-"+suffix, "expired-"+suffix
	for token, expiresAt := range map[string]time.Time{
		valid:   time.Now().Add(time.Hour),
		expired: time.Now().Add(-time.Hour),
	} {
		_, err := q.AddToken(ctx, database.AddTokenParams{SatelliteID: sat.ID, Token: token, ExpiresAt: expiresAt})
		require.NoError(t, err)
	}
	return q, sat.ID, valid, expired
}

func TestClaimZTRToken_SecondClaimIsInvalid(t *testing.T) {
	pending.Plan(t, "D7", "Ztr reads the token without claiming it, so concurrent registrations all succeed")
	q, satelliteID, valid, _ := newTokenDB(t)
	ctx := context.Background()

	id, err := claimZTRToken(ctx, q, valid)
	require.NoError(t, err)
	require.Equal(t, satelliteID, id)

	_, err = claimZTRToken(ctx, q, valid)
	require.ErrorIs(t, err, errTokenInvalid)
	require.ErrorIs(t, err, errTokenConsumed)
}

func TestClaimZTRToken_UnknownIsInvalid(t *testing.T) {
	pending.Plan(t, "D7", "Ztr reads the token without claiming it, so concurrent registrations all succeed")
	q, _, _, _ := newTokenDB(t)

	_, err := claimZTRToken(context.Background(), q, "unknown-token")

	require.ErrorIs(t, err, errTokenInvalid)
	require.NotErrorIs(t, err, errTokenConsumed)
}

func TestClaimZTRToken_ExpiredIsExpired(t *testing.T) {
	pending.Plan(t, "D7", "Ztr reads the token without claiming it, so concurrent registrations all succeed")
	q, _, _, expired := newTokenDB(t)

	_, err := claimZTRToken(context.Background(), q, expired)

	require.ErrorIs(t, err, errTokenExpired)
}

func TestUnclaimZTRToken_AllowsRetry(t *testing.T) {
	pending.Plan(t, "D7", "Ztr reads the token without claiming it, so concurrent registrations all succeed")
	q, satelliteID, valid, _ := newTokenDB(t)
	ctx := context.Background()
	_, err := claimZTRToken(ctx, q, valid)
	require.NoError(t, err)

	require.NoError(t, unclaimZTRToken(ctx, q, valid))

	id, err := claimZTRToken(ctx, q, valid)
	require.NoError(t, err)
	require.Equal(t, satelliteID, id)
}

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
	pending.Plan(t, "D7", "Ztr ignores consumed_at, so a token another registration has claimed still works")
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
