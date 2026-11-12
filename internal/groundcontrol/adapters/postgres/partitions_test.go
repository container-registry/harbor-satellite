package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

func TestPartitionMaintainer_CreatesAheadDropsExpired(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	m, err := postgres.NewPartitionMaintainer(db.DB, fleettest.Clock{T: hotNow}, 7, 3)
	require.NoError(t, err)
	require.Equal(t, "status-partitions", m.Name())

	require.NoError(t, m.RunOnce(ctx), "a no-op while satellite_status is not partitioned")

	partitions := func() []string {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
			WHERE i.inhparent = 'satellite_status'::regclass AND c.relname <> 'satellite_status_default' ORDER BY 1`)
		require.NoError(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			out = append(out, name)
		}
		require.NoError(t, rows.Err())
		return out
	}
	require.Equal(t, []string{
		"satellite_status_20261015", "satellite_status_20261016", "satellite_status_20261017", "satellite_status_20261018",
	}, partitions(), "today and three days ahead; needs the pending partition DDL applied")
}

func TestPartitionMaintainer_MovesDefaultRowsAndDropsByRetention(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	sat, err := database.New(db.DB).CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	for _, at := range []time.Time{hotNow.Add(-30 * 24 * time.Hour), hotNow} {
		_, err := db.ExecContext(ctx, "INSERT INTO satellite_status (satellite_id, activity, reported_at, created_at) VALUES ($1, 'idle', $2, $2)", sat.ID, at)
		require.NoError(t, err)
	}
	m, err := postgres.NewPartitionMaintainer(db.DB, fleettest.Clock{T: hotNow}, 7, 1)
	require.NoError(t, err)

	require.NoError(t, m.RunOnce(ctx))
	count := func(table string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		return n
	}
	require.Equal(t, 1, count("satellite_status_20261015"), "today's row moved out of the default partition")
	require.Zero(t, count("satellite_status_default"), "the row older than the retention is deleted")

	later, err := postgres.NewPartitionMaintainer(db.DB, fleettest.Clock{T: hotNow.Add(10 * 24 * time.Hour)}, 7, 1)
	require.NoError(t, err)
	require.NoError(t, later.RunOnce(ctx))
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass('satellite_status_20261015') IS NOT NULL").Scan(&exists))
	require.False(t, exists, "a partition past the retention is dropped")
	require.NoError(t, later.RunOnce(ctx), "idempotent")
}
