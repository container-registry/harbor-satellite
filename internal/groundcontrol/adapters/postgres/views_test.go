package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingViews = "the postgres view repository is a stub; ADR-0006 lifecycle is not implemented"

// A second worker holding the StaleDetector lock makes MarkStale a no-op,
// so two workers never append SatelliteStale twice.
func TestViewRepo_MarkStaleTakesLock(t *testing.T) {
	pending.Plan(t, "4.3", pendingViews)
	db := dbtest.New(t)
	ctx := context.Background()
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	sat, err := database.New(db.DB).CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO satellite_view (satellite_id, state, last_seen, heartbeat_interval) VALUES ($1, 'online', $2, '@every 00h00m30s')", sat.ID, hotNow.Add(-time.Hour))
	require.NoError(t, err)
	holder, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Close() })
	tx, err := holder.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", advisorylock.StaleDetector)
	require.NoError(t, err)

	var stale []fleet.StaleSatellite
	require.NoError(t, repo.WithTx(ctx, func(tx fleet.Tx) error {
		var err error
		stale, err = tx.Views().MarkStale(ctx, hotNow, 15*time.Minute, 10*time.Second)
		return err
	}))
	require.Empty(t, stale, "the lock is held elsewhere")

	require.NoError(t, tx.Rollback())
	require.NoError(t, repo.WithTx(ctx, func(tx fleet.Tx) error {
		var err error
		stale, err = tx.Views().MarkStale(ctx, hotNow, 15*time.Minute, 10*time.Second)
		return err
	}))
	require.Len(t, stale, 1)
	require.Equal(t, fleet.SatelliteID(sat.ID), stale[0].Satellite)
}

func viewState(t *testing.T, repo *postgres.Repository, id fleet.SatelliteID) fleet.LifecycleState {
	t.Helper()
	v, err := repo.Autocommit().Views().Get(context.Background(), id)
	require.NoError(t, err)
	return v.State
}
