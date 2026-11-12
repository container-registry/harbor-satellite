package postgres_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

func advisoryLocks(t *testing.T, db *dbtest.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())").Scan(&n))
	return n
}

// sql.Conn.Close hands a healthy session back to the pool, locks and all,
// so Close must release what the locker still holds.
func TestLockers_CloseReleasesHeldLocks(t *testing.T) {
	ctx := context.Background()
	t.Run("aggregate", func(t *testing.T) {
		db := dbtest.New(t)
		l, err := postgres.NewAggregateLocker(ctx, db.DB)
		require.NoError(t, err)
		_, ok, err := l.TryLock(ctx, fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7})
		require.NoError(t, err)
		require.True(t, ok)
		_, ok, err = l.TryLock(ctx, fleet.Aggregate{Type: fleet.AggregateGroup, ID: 3})
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, 2, advisoryLocks(t, db))

		require.NoError(t, l.Close())
		require.Zero(t, advisoryLocks(t, db), "Close left advisory locks on the pooled session")
	})
	t.Run("subscription", func(t *testing.T) {
		db := dbtest.New(t)
		l, err := postgres.NewSubscriptionLocker(ctx, db.DB)
		require.NoError(t, err)
		_, ok, err := l.TryLock(ctx, fleet.WebhookID(4))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, 1, advisoryLocks(t, db))

		require.NoError(t, l.Close())
		require.Zero(t, advisoryLocks(t, db), "Close left advisory locks on the pooled session")
	})
}
