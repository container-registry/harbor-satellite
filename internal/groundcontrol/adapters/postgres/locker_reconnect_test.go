package postgres_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

var lockerNames = []string{"aggregate", "subscription"}

type tryLocker func(ctx context.Context, key int64) (func(), bool, error)

func lockers(t *testing.T, db *dbtest.DB) map[string]tryLocker {
	t.Helper()
	ctx := context.Background()
	agg, err := postgres.NewAggregateLocker(ctx, db.DB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = agg.Close() })
	sub, err := postgres.NewSubscriptionLocker(ctx, db.DB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	return map[string]tryLocker{
		"aggregate": func(ctx context.Context, key int64) (func(), bool, error) {
			return agg.TryLock(ctx, fleet.Aggregate{Type: fleet.AggregateSatellite, ID: key})
		},
		"subscription": func(ctx context.Context, key int64) (func(), bool, error) {
			return sub.TryLock(ctx, fleet.WebhookID(key))
		},
	}
}

// terminateLockSessions ends every session of the test database that holds
// an advisory lock, as a PostgreSQL restart or an administrator would.
func terminateLockSessions(t *testing.T, db *dbtest.DB) {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT count(pg_terminate_backend(pid)) FROM pg_locks WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())").Scan(&n))
	require.Equal(t, 1, n)
}

// A locker whose session is gone must take its next lock on a new session
// instead of refusing every lock until Ground Control restarts.
func TestLockers_ReopenTheSessionAfterItIsLost(t *testing.T) {
	ctx := context.Background()
	t.Run("after a failed unlock", func(t *testing.T) {
		for _, name := range lockerNames {
			t.Run(name, func(t *testing.T) {
				db := dbtest.New(t)
				lock := lockers(t, db)[name]
				unlock, ok, err := lock(ctx, 1)
				require.NoError(t, err)
				require.True(t, ok)
				terminateLockSessions(t, db)

				unlock()

				_, ok, err = lock(ctx, 2)
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, 1, advisoryLocks(t, db))
			})
		}
	})
	t.Run("after a failed lock", func(t *testing.T) {
		for _, name := range lockerNames {
			t.Run(name, func(t *testing.T) {
				db := dbtest.New(t)
				lock := lockers(t, db)[name]
				staleUnlock, ok, err := lock(ctx, 1)
				require.NoError(t, err)
				require.True(t, ok)
				terminateLockSessions(t, db)
				_, _, err = lock(ctx, 2)
				require.Error(t, err, "the lost session answers the first lock")

				_, ok, err = lock(ctx, 1)
				require.NoError(t, err)
				require.True(t, ok, "the lost session took its locks with it")
				staleUnlock()
				require.Equal(t, 1, advisoryLocks(t, db), "an unlock of the lost session must not release the new holder")
				_, ok, err = lock(ctx, 1)
				require.NoError(t, err)
				require.False(t, ok, "the new holder still holds it")
			})
		}
	})
}
