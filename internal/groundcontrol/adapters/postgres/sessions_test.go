package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

var hotNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func TestSessionStore_FindByHashAndName(t *testing.T) {
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

func sessionsOf(t *testing.T, db *dbtest.DB, satellite fleet.SatelliteID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT count(*) FROM satellite_sessions WHERE satellite_id = $1", int32(satellite)).Scan(&n))
	return n
}

// A renewal that commits while a re-ZTR (Replace) or an invalidation
// (DeleteForSatellite) runs must not leave its successor behind: the old
// token holder would keep a session for a whole TTL after the satellite was
// re-onboarded.
func TestSessionStore_ReplaceAndDeleteWinOverAConcurrentRenewal(t *testing.T) {
	for name, wipe := range map[string]func(*postgres.SessionStore, fleet.SatelliteID) error{
		"replace": func(s *postgres.SessionStore, id fleet.SatelliteID) error {
			return s.Replace(context.Background(), id, sha256.Sum256([]byte("hss_reztr")), hotNow.Add(720*time.Hour))
		},
		"delete": func(s *postgres.SessionStore, id fleet.SatelliteID) error {
			return s.DeleteForSatellite(context.Background(), id)
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := dbtest.New(t)
			ctx := context.Background()
			sat, err := database.New(db.DB).CreateSatellite(ctx, "edge-01")
			require.NoError(t, err)
			id := fleet.SatelliteID(sat.ID)
			store, err := postgres.NewSessionStore(db.DB)
			require.NoError(t, err)
			require.NoError(t, store.Replace(ctx, id, sha256.Sum256([]byte("hss_old")), hotNow.Add(time.Hour)))
			old, err := store.FindByHash(ctx, sha256.Sum256([]byte("hss_old")))
			require.NoError(t, err)

			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			require.NoError(t, database.New(db.DB).WithTx(tx).RenewSatelliteSession(ctx, database.RenewSatelliteSessionParams{
				SatelliteID: int32(id), TokenHash: func() []byte { h := sha256.Sum256([]byte("hss_successor")); return h[:] }(),
				ExpiresAt: hotNow.Add(720 * time.Hour), RenewedFrom: sql.NullInt64{Int64: old.SessionID, Valid: true},
			}))
			done := make(chan error, 1)
			go func() { done <- wipe(store, id) }()
			select {
			case doneErr := <-done:
				t.Fatalf("%s did not wait for the open renewal: %v", name, doneErr)
			case <-time.After(200 * time.Millisecond):
			}
			require.NoError(t, tx.Commit())
			require.NoError(t, <-done)

			_, err = store.FindByHash(ctx, sha256.Sum256([]byte("hss_successor")))
			require.ErrorIs(t, err, fleet.ErrNotFound, "the renewed successor of the old session survived")
			want := map[string]int{"replace": 1, "delete": 0}[name]
			require.Equal(t, want, sessionsOf(t, db, id))
		})
	}
}
