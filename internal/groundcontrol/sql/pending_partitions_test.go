package migrations_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func relationKind(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var kind string
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT relkind::TEXT FROM pg_class WHERE oid = to_regclass($1)", table).Scan(&kind))
	return kind
}

func statusCount(t *testing.T, db *sql.DB, satellite int32) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT count(*) FROM satellite_status WHERE satellite_id = $1", satellite).Scan(&n))
	return n
}

// The pending DDL is not applied anywhere, so this keeps it valid until it
// moves into sql/schema.
func TestPendingStatusPartitionsSQL_AppliesAndReverts(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	q := database.New(db.DB)
	schema, latest := withPendingMigration(t, "satellite_status_partitions")
	p := provider(t, db.DB, schema)
	insert := func(satellite int32) database.SatelliteStatus {
		t.Helper()
		row, err := q.InsertSatelliteStatus(ctx, database.InsertSatelliteStatusParams{SatelliteID: satellite, Activity: "idle", ReportedAt: time.Now().UTC()})
		require.NoError(t, err)
		return row
	}
	kept, err := q.CreateSatellite(ctx, "kept")
	require.NoError(t, err)
	deleted, err := q.CreateSatellite(ctx, "deleted")
	require.NoError(t, err)
	before := insert(kept.ID)
	insert(deleted.ID)

	_, err = p.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, "p", relationKind(t, db.DB, "satellite_status"), "partitioned")
	require.Equal(t, "r", relationKind(t, db.DB, "satellite_status_default"))
	require.Equal(t, 1, statusCount(t, db.DB, kept.ID), "rows are copied")
	after := insert(kept.ID)
	require.Greater(t, after.ID, before.ID, "the id sequence continues")
	latestStatus, err := q.GetLatestSatelliteStatus(ctx, kept.ID)
	require.NoError(t, err)
	require.Equal(t, after.ID, latestStatus.ID)
	_, err = db.ExecContext(ctx, "DELETE FROM satellites WHERE id = $1", deleted.ID)
	require.NoError(t, err)
	require.Zero(t, statusCount(t, db.DB, deleted.ID), "the foreign key cascades")
	_, err = q.InsertSatelliteStatus(ctx, database.InsertSatelliteStatusParams{SatelliteID: 9999, Activity: "idle", ReportedAt: time.Now().UTC()})
	require.ErrorContains(t, err, "satellite_status_satellite_id_fkey")

	_, err = p.DownTo(ctx, latest)
	require.NoError(t, err)
	require.Equal(t, "r", relationKind(t, db.DB, "satellite_status"))
	require.Equal(t, 2, statusCount(t, db.DB, kept.ID), "rows are copied back")
	insert(kept.ID)
	var indexes int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_indexes WHERE tablename = 'satellite_status' AND indexname IN ('satellite_status_pkey', 'idx_satellite_status_satellite_id', 'idx_satellite_status_created_at')").Scan(&indexes))
	require.Equal(t, 3, indexes)
}
