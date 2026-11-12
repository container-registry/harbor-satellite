package postgres_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingSessions = "the postgres session store is a stub; Ground Control issues no satellite sessions"

var hotNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func TestSessionStore_FindByHashAndName(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	db := dbtest.New(t)
	ctx := context.Background()
	sat, err := database.New(db.DB).CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	id := fleet.SatelliteID(sat.ID)
	store, err := postgres.NewSessionStore(db.DB)
	require.NoError(t, err)
	hash := sha256.Sum256([]byte("hss_first"))
	expires := hotNow.Add(720 * time.Hour)

	byName, err := store.FindByName(ctx, "edge-01", hotNow)
	require.NoError(t, err)
	require.Equal(t, id, byName.Satellite)
	require.Equal(t, fleet.StatePending, byName.State, "no satellite_view row is pending")
	require.False(t, byName.HasSession)

	added, err := store.AddIfNone(ctx, id, hash, expires, hotNow)
	require.NoError(t, err)
	require.True(t, added)
	added, err = store.AddIfNone(ctx, id, sha256.Sum256([]byte("hss_second")), expires, hotNow)
	require.NoError(t, err)
	require.False(t, added, "a satellite with a session gets no second one")

	auth, err := store.FindByHash(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, id, auth.Satellite)
	require.Equal(t, "edge-01", auth.SatelliteName)
	require.NotZero(t, auth.SessionID)
	require.True(t, expires.Equal(auth.ExpiresAt))
	byName, err = store.FindByName(ctx, "edge-01", hotNow)
	require.NoError(t, err)
	require.True(t, byName.HasSession)

	_, err = store.FindByHash(ctx, sha256.Sum256([]byte("hss_unknown")))
	require.ErrorIs(t, err, fleet.ErrNotFound)
	_, err = store.FindByName(ctx, "edge-99", hotNow)
	require.ErrorIs(t, err, fleet.ErrNotFound)

	require.NoError(t, store.DeleteForSatellite(ctx, id))
	_, err = store.FindByHash(ctx, hash)
	require.ErrorIs(t, err, fleet.ErrNotFound)
}
