package dbtest_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestNewAppliesAllMigrationsAndInsertsSatellite(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	latest, err := dbtest.LatestMigration()
	require.NoError(t, err)
	require.Positive(t, latest)

	var applied int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT max(version_id) FROM goose_db_version").Scan(&applied))
	require.Equal(t, latest, applied)

	sat, err := database.New(db.DB).CreateSatellite(ctx, "smoke-satellite")
	require.NoError(t, err)
	require.Equal(t, "smoke-satellite", sat.Name)
	require.Positive(t, sat.ID)
}

func TestDatabasesAreIsolated(t *testing.T) {
	a := dbtest.New(t)
	b := dbtest.New(t)
	ctx := context.Background()

	_, err := database.New(a.DB).CreateSatellite(ctx, "only-in-a")
	require.NoError(t, err)

	var n int
	require.NoError(t, b.QueryRowContext(ctx, "SELECT count(*) FROM satellites").Scan(&n))
	require.Zero(t, n)
}

func TestMeasureStatementsCountsQueriesOfThisDatabaseOnly(t *testing.T) {
	db := dbtest.New(t)
	other := dbtest.New(t)
	ctx := context.Background()
	q := database.New(db.DB)

	got := db.MeasureStatements(t, func() {
		_, err := q.CreateSatellite(ctx, "sat-1")
		require.NoError(t, err)
		_, err = q.GetSatelliteByName(ctx, "sat-1")
		require.NoError(t, err)
		_, err = q.GetSatelliteByName(ctx, "sat-1")
		require.NoError(t, err)
		_, err = database.New(other.DB).ListSatellites(ctx)
		require.NoError(t, err)
	})

	require.Equal(t, map[string]int64{"CreateSatellite": 1, "GetSatelliteByName": 2}, got.Calls, got.String())
	require.EqualValues(t, 3, got.Total)
}
