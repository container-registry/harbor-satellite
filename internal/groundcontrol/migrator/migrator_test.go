package migrator_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/migrator"
	"github.com/stretchr/testify/require"
)

// GC replicas and workers start together against one database; every one
// of them must come up, not only the one that wins the race to migrate.
func TestRun_ConcurrentProcessesAllSucceed(t *testing.T) {
	db := dbtest.NewEmpty(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const processes = 4
	errs := make([]error, processes)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range processes {
		wg.Go(func() {
			conn, err := sql.Open(dbpool.DriverName, db.URL)
			if err != nil {
				errs[i] = err
				return
			}
			defer conn.Close()
			if errs[i] = conn.PingContext(ctx); errs[i] != nil {
				return
			}
			<-start
			errs[i] = migrator.Run(ctx, conn)
		})
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "process %d", i)
	}

	latest, err := dbtest.LatestMigration()
	require.NoError(t, err)
	var applied, rows, distinct int64
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT max(version_id), count(*), count(DISTINCT version_id) FROM goose_db_version WHERE version_id > 0").Scan(&applied, &rows, &distinct))
	require.Equal(t, latest, applied)
	require.Equal(t, distinct, rows, "a migration was recorded twice")
}
