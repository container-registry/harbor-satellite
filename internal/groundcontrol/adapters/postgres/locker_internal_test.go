package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

// The unlock-error path discards the session; its locks must end with it.
func TestDiscard_EndsTheSessionAndItsLocks(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	db.SetMaxIdleConns(4)
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	var pid int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid))
	_, err = conn.ExecContext(ctx, "SELECT pg_advisory_lock(1, 2)")
	require.NoError(t, err)

	require.NoError(t, discard(conn))

	require.Eventually(t, func() bool {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE pid = $1", pid).Scan(&n))
		return n == 0
	}, 5*time.Second, 10*time.Millisecond, "the discarded session is still open")
	var locks int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())").Scan(&locks))
	require.Zero(t, locks)
}
