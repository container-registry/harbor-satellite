package migrator

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

func withDatabaseEnv(t *testing.T, db env.Database) {
	t.Helper()
	prev := env.GC.Database
	t.Cleanup(func() { env.GC.Database = prev })
	env.GC.Database = db
}

func appliedVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v int64
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(&v))
	return v
}

func TestDoMigrationsAppliesTheSchemaOnce(t *testing.T) {
	db := dbtest.NewEmpty(t)
	withDatabaseEnv(t, env.Database{URLAPI: db.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	require.NoError(t, DoMigrations(ctx))
	latest, err := dbtest.LatestMigration()
	require.NoError(t, err)
	require.Equal(t, latest, appliedVersion(t, db.DB))

	var rows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM goose_db_version").Scan(&rows))
	require.NoError(t, DoMigrations(ctx), "a second start is a no-op")
	var again int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM goose_db_version").Scan(&again))
	require.Equal(t, rows, again)
}

func TestDoMigrationsBuildsTheDSNFromParts(t *testing.T) {
	db := dbtest.NewEmpty(t)
	u, err := url.Parse(db.URL)
	require.NoError(t, err)
	password, _ := u.User.Password()
	withDatabaseEnv(t, env.Database{
		Host: u.Hostname(), Port: u.Port(), Username: u.User.Username(), Password: password,
		Database: u.Path[1:], SSLMode: "disable",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, DoMigrations(ctx))
	latest, err := dbtest.LatestMigration()
	require.NoError(t, err)
	require.Equal(t, latest, appliedVersion(t, db.DB))
}

func TestDoMigrationsRejectsAnInvalidSSLMode(t *testing.T) {
	withDatabaseEnv(t, env.Database{Host: "127.0.0.1", Port: "5432", SSLMode: "sometimes"})
	require.ErrorContains(t, DoMigrations(context.Background()), `DB_SSLMODE "sometimes"`)
}

func TestDoMigrationsGivesUpWhenPostgresNeverAnswers(t *testing.T) {
	withDatabaseEnv(t, env.Database{URLAPI: "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	require.ErrorIs(t, DoMigrations(ctx), context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second, "the caller's deadline bounds the readiness wait")
}

func TestCheckServerFailsOnAClosedConnection(t *testing.T) {
	conn, err := sql.Open(dbpool.DriverName, "postgres://u:p@127.0.0.1:1/none?sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.ErrorContains(t, CheckServer(context.Background(), conn), "read PostgreSQL server settings")
}

func TestRunReportsAFailedMigration(t *testing.T) {
	db := dbtest.NewEmpty(t)
	ctx := context.Background()
	// Without its version rows goose applies migration 001 again, and its
	// tables already exist.
	require.NoError(t, Run(ctx, db.DB))
	_, err := db.ExecContext(ctx, "DELETE FROM goose_db_version WHERE version_id > 0")
	require.NoError(t, err)
	require.Error(t, Run(ctx, db.DB))
}
