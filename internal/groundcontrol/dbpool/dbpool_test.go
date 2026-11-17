package dbpool_test

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func singleDSNDatabase() env.Database {
	return env.Database{Host: "db.example", Port: "5432", Username: "gc", Password: "secret", Database: "groundcontrol", SSLMode: "disable"}
}

func TestConfigFromEnv_PlanesWithoutURLUseTheAPIPool(t *testing.T) {
	cfg, err := dbpool.ConfigFromEnv(singleDSNDatabase())
	require.NoError(t, err)

	u, err := url.Parse(cfg.API.DSN)
	require.NoError(t, err)
	require.Equal(t, "db.example:5432", u.Host)
	require.Equal(t, "/groundcontrol", u.Path)
	require.Equal(t, "groundcontrol-api", cfg.API.ApplicationName)
	require.Empty(t, cfg.Fleet.DSN, "a plane without DB_URL_FLEET shares the API pool")
	require.Empty(t, cfg.Async.DSN, "a plane without DB_URL_ASYNC shares the API pool")
}

func TestConfigFromEnv_PlaneOverride(t *testing.T) {
	db := singleDSNDatabase()
	db.URLAPI = "postgres://api@db-api.example:5432/groundcontrol"
	db.URLFleet = "postgres://fleet@db-fleet.example:5432/groundcontrol"
	db.FleetPool = env.Pool{MaxOpenConns: 20, StatementTimeout: time.Second, LockTimeout: 500 * time.Millisecond}

	cfg, err := dbpool.ConfigFromEnv(db)
	require.NoError(t, err)
	require.Equal(t, db.URLAPI, cfg.API.DSN)
	require.Equal(t, db.URLFleet, cfg.Fleet.DSN)
	require.Equal(t, 20, cfg.Fleet.MaxOpenConns)
	require.Equal(t, time.Second, cfg.Fleet.StatementTimeout)
	require.Equal(t, 500*time.Millisecond, cfg.Fleet.LockTimeout)
	require.Equal(t, "groundcontrol-fleet", cfg.Fleet.ApplicationName)
	require.Empty(t, cfg.Async.DSN)
}

func TestWithRuntimeParams_EncodesOptions(t *testing.T) {
	dsn, err := dbpool.WithRuntimeParams("postgres://gc:secret@db.example:5432/groundcontrol?sslmode=disable", dbpool.PoolConfig{
		StatementTimeout:       5 * time.Second,
		LockTimeout:            time.Second,
		IdleInTxSessionTimeout: 30 * time.Second,
		ApplicationName:        "groundcontrol-fleet",
	})
	require.NoError(t, err)

	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	require.Equal(t, "disable", q.Get("sslmode"))
	require.Equal(t, "groundcontrol-fleet", q.Get("application_name"))
	require.Equal(t, "-c statement_timeout=5000 -c lock_timeout=1000 -c idle_in_transaction_session_timeout=30000", q.Get("options"))
}

func show(t *testing.T, db *sql.DB, setting string) string {
	t.Helper()
	var v string
	require.NoError(t, db.QueryRowContext(context.Background(), "SHOW "+setting).Scan(&v))
	return v
}

func TestOpen_PoolsApplyRuntimeParams(t *testing.T) {
	db := dbtest.New(t)
	pools, err := dbpool.Open(context.Background(), dbpool.Config{
		API: dbpool.PoolConfig{DSN: db.URL, ApplicationName: "groundcontrol-api"},
		Fleet: dbpool.PoolConfig{
			DSN: db.URL, ApplicationName: "groundcontrol-fleet",
			StatementTimeout: time.Second, LockTimeout: 500 * time.Millisecond, IdleInTxSessionTimeout: 2 * time.Second,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pools.Close()) })

	require.Equal(t, "1s", show(t, pools.Fleet, "statement_timeout"))
	require.Equal(t, "500ms", show(t, pools.Fleet, "lock_timeout"))
	require.Equal(t, "2s", show(t, pools.Fleet, "idle_in_transaction_session_timeout"))
	require.Equal(t, "groundcontrol-fleet", show(t, pools.Fleet, "application_name"))
	require.Equal(t, "0", show(t, pools.API, "statement_timeout"))
	require.Equal(t, "groundcontrol-api", show(t, pools.API, "application_name"))
}

func TestOpen_PlanesWithoutDSNShareTheAPIPool(t *testing.T) {
	db := dbtest.New(t)
	pools, err := dbpool.Open(context.Background(), dbpool.Config{API: dbpool.PoolConfig{DSN: db.URL}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pools.Close()) })

	require.Same(t, pools.API, pools.Fleet)
	require.Same(t, pools.API, pools.Async)
}

// Plan 3.1 assumed the driver had to change for cancellation to reach the
// server; lib/pq already sends a cancel request.
func TestLibPQCancelsQueryOnContextCancel(t *testing.T) {
	db := dbtest.New(t)
	conn, err := sql.Open(dbpool.DriverName, db.URL)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = conn.ExecContext(ctx, "SELECT pg_sleep(20)")
	elapsed := time.Since(start)
	t.Logf("ExecContext returned after %v: %v", elapsed, err)

	require.Less(t, elapsed, 5*time.Second)
	var pqErr *pq.Error
	require.ErrorAs(t, err, &pqErr)
	require.Equal(t, "57014", string(pqErr.Code), "query_canceled")

	require.Eventually(t, func() bool {
		var running int
		require.NoError(t, db.QueryRowContext(context.Background(),
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND query LIKE 'SELECT pg_sleep(20)%' AND state = 'active'").Scan(&running))
		return running == 0
	}, 5*time.Second, 50*time.Millisecond, "pg_sleep still runs on the server after the context ended")
}

func TestConfigFromEnv_PlaneSettingsWithoutURLGetTheirOwnPool(t *testing.T) {
	db := singleDSNDatabase()
	db.AsyncPool = env.Pool{MaxOpenConns: 2, StatementTimeout: time.Minute}

	cfg, err := dbpool.ConfigFromEnv(db)
	require.NoError(t, err)
	require.Equal(t, cfg.API.DSN, cfg.Async.DSN, "DB_ASYNC_* without DB_URL_ASYNC opens a pool on the API DSN")
	require.Equal(t, 2, cfg.Async.MaxOpenConns)
	require.Equal(t, "groundcontrol-async", cfg.Async.ApplicationName)
	require.Empty(t, cfg.Fleet.DSN)
}

func TestConfigFromEnv_RejectsInvalidSSLMode(t *testing.T) {
	db := singleDSNDatabase()
	db.SSLMode = "verify"
	_, err := dbpool.ConfigFromEnv(db)
	require.ErrorContains(t, err, "DB_SSLMODE")
}

func TestWithRuntimeParams_KeepsExistingOptionsFirst(t *testing.T) {
	dsn, err := dbpool.WithRuntimeParams("postgres://gc@db.example/gc?options=-c%20search_path%3Dgc", dbpool.PoolConfig{LockTimeout: time.Second})
	require.NoError(t, err)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "-c search_path=gc -c lock_timeout=1000", u.Query().Get("options"))

	_, err = dbpool.WithRuntimeParams("mysql://db.example/gc", dbpool.PoolConfig{})
	require.Error(t, err)
}

func TestOpen_SeparatePoolsCloseOnce(t *testing.T) {
	db := dbtest.New(t)
	pools, err := dbpool.Open(context.Background(), dbpool.Config{
		API:   dbpool.PoolConfig{DSN: db.URL, ApplicationName: "groundcontrol-api"},
		Async: dbpool.PoolConfig{DSN: db.URL, ApplicationName: "groundcontrol-async", MaxOpenConns: 1},
	})
	require.NoError(t, err)
	require.NotSame(t, pools.API, pools.Async)
	require.Same(t, pools.API, pools.Fleet)
	require.Equal(t, "groundcontrol-async", show(t, pools.Async, "application_name"))
	require.Equal(t, 1, pools.Async.Stats().MaxOpenConnections)
	require.NoError(t, pools.Close())
	require.Error(t, pools.API.PingContext(context.Background()), "closed")
}
