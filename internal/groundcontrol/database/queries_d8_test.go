package database_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestTryAdvisoryXactLock_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	const lockID = 12345
	try := func(tx *sql.Tx) bool {
		t.Helper()
		acquired, err := database.New(db.DB).WithTx(tx).TryAdvisoryXactLock(ctx, lockID)
		require.NoError(t, err)
		return acquired
	}

	tx1, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.True(t, try(tx1), "tx1 takes the free lock")

	tx2, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.False(t, try(tx2), "tx2 gets the lock while tx1 holds it")
	require.NoError(t, tx2.Rollback())

	require.NoError(t, tx1.Commit())
	tx3, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.True(t, try(tx3), "the lock is still held after tx1 committed")
	require.NoError(t, tx3.Commit())

	var held int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks
WHERE locktype = 'advisory' AND objid = $1
  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`, lockID).Scan(&held))
	require.Zero(t, held, "the lock outlived its transactions")
}
