package postgres_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestPartitionMaintainer_CreatesAheadDropsExpired(t *testing.T) {
	pending.Plan(t, "4.5", "satellite_status is not partitioned; the cleanup deletes rows")
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
