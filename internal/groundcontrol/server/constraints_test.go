package server

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Tests for plan items 3.4 (constraints) and 3.5 (housekeeping). The DDL
// they need is in sql/pending until 3.4 moves it into sql/schema.

func requireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pqErr *pq.Error
	require.ErrorAs(t, err, &pqErr, "want SQLSTATE %s", code)
	require.Equal(t, code, string(pqErr.Code), pqErr.Message)
}

func insertSatellite(t *testing.T, db *sql.DB) int32 {
	t.Helper()
	var id int32
	require.NoError(t, db.QueryRowContext(context.Background(), "INSERT INTO satellites (name) VALUES ('edge-01') RETURNING id").Scan(&id))
	return id
}

func TestConstraint_RobotPerSatellite(t *testing.T) {
	pending.Plan(t, "3.4", "robot_accounts allows several robots per satellite")
	db := dbtest.New(t)
	ctx := context.Background()
	satellite := insertSatellite(t, db.DB)
	insert := "INSERT INTO robot_accounts (robot_name, robot_secret_hash, robot_id, satellite_id) VALUES ($1, 'hash', $2, $3)"

	_, err := db.ExecContext(ctx, insert, "robot$edge-01", "1", satellite)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, insert, "robot$edge-01-again", "2", satellite)
	requireSQLState(t, err, "23505")
}

func TestConstraint_ConfigRestrict(t *testing.T) {
	pending.Plan(t, "3.4", "deleting a config cascades to satellite_configs instead of being refused")
	db := dbtest.New(t)
	ctx := context.Background()
	satellite := insertSatellite(t, db.DB)
	var config int32
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO configs (config_name, registry_url, config) VALUES ('edge-config', 'https://harbor.example', '{}') RETURNING id").Scan(&config))
	_, err := db.ExecContext(ctx, "INSERT INTO satellite_configs (satellite_id, config_id) VALUES ($1, $2)", satellite, config)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, "DELETE FROM configs WHERE id = $1", config)
	requireSQLState(t, err, "23503")
}

func TestConstraint_HeartbeatCheck(t *testing.T) {
	pending.Plan(t, "3.4", "satellites.heartbeat_interval accepts any string")
	db := dbtest.New(t)
	ctx := context.Background()
	satellite := insertSatellite(t, db.DB)
	update := "UPDATE satellites SET heartbeat_interval = $1 WHERE id = $2"

	_, err := db.ExecContext(ctx, update, "@every 00h00m30s", satellite)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, update, "@every 30s", satellite)
	requireSQLState(t, err, "23514")
}

func TestConstraint_Timestamptz(t *testing.T) {
	pending.Plan(t, "3.4", "timestamps are TIMESTAMP without time zone")
	db := dbtest.New(t)
	rows, err := db.QueryContext(context.Background(), `SELECT table_name, column_name FROM information_schema.columns
WHERE table_schema = 'public' AND table_name <> 'goose_db_version' AND data_type = 'timestamp without time zone'
ORDER BY table_name, column_name`)
	require.NoError(t, err)
	defer rows.Close()
	var naive []string
	for rows.Next() {
		var table, column string
		require.NoError(t, rows.Scan(&table, &column))
		naive = append(naive, table+"."+column)
	}
	require.NoError(t, rows.Err())
	require.Empty(t, naive)
}

func TestUpdateConfig_ConcurrentPatchOneGets409(t *testing.T) {
	pending.Plan(t, "3.4", "concurrent config updates both succeed and the later one overwrites the earlier one")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.harbor.Gate(harbortest.RoutePutManifest, 2, 5*time.Second)

	reqs := []*http.Request{
		h.newRequest(http.MethodPatch, "/api/configs/edge-config", map[string]any{"app_config": map[string]any{"log_level": "debug"}}, h.admin),
		h.newRequest(http.MethodPatch, "/api/configs/edge-config", map[string]any{"app_config": map[string]any{"log_level": "warn"}}, h.admin),
	}
	results := make([]response, len(reqs))
	errs := make([]error, len(reqs))
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Go(func() { results[i], errs[i] = send(req) })
	}
	wg.Wait()

	statuses := make([]int, len(reqs))
	for i := range reqs {
		require.NoError(t, errs[i])
		statuses[i] = results[i].Status
	}
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, statuses,
		"responses: %s / %s", results[0].Body, results[1].Body)
}

func TestCleanup_PurgesExpiredTokensSessionsAttempts(t *testing.T) {
	pending.Plan(t, "3.5", "the cleanup never calls DeleteExpiredTokens or DeleteExpiredSessions and keeps login_attempts forever")
	db := dbtest.New(t)
	ctx := context.Background()
	s := &Server{db: db.DB, dbQueries: database.New(db.DB)}
	satellite := insertSatellite(t, db.DB)
	var user int32
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (username, password_hash, role) VALUES ('operator', 'hash', 'admin') RETURNING id").Scan(&user))

	// Days, not hours, apart so the TIMESTAMP columns compare the same in any
	// time zone the database and the test process run in.
	for _, stmt := range []string{
		"INSERT INTO satellite_token (satellite_id, token, expires_at) VALUES ($1, 'expired-token', NOW() - INTERVAL '3 days')",
		"INSERT INTO satellite_token (satellite_id, token, expires_at) VALUES ($1, 'valid-token', NOW() + INTERVAL '3 days')",
	} {
		_, err := db.ExecContext(ctx, stmt, satellite)
		require.NoError(t, err)
	}
	for _, stmt := range []string{
		"INSERT INTO sessions (user_id, token, expires_at) VALUES ($1, 'expired-session', NOW() - INTERVAL '3 days')",
		"INSERT INTO sessions (user_id, token, expires_at) VALUES ($1, 'valid-session', NOW() + INTERVAL '3 days')",
	} {
		_, err := db.ExecContext(ctx, stmt, user)
		require.NoError(t, err)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO login_attempts (username, failed_count, last_attempt) VALUES
('stale', 2, NOW() - INTERVAL '30 days'), ('recent', 2, NOW())`)
	require.NoError(t, err)

	s.runCleanupWithLock(ctx, defaultRetentionDays)

	require.Equal(t, []string{"valid-token"}, columnValues(t, db.DB, "SELECT token FROM satellite_token ORDER BY token"))
	require.Equal(t, []string{"valid-session"}, columnValues(t, db.DB, "SELECT token FROM sessions ORDER BY token"))
	require.Equal(t, []string{"recent"}, columnValues(t, db.DB, "SELECT username FROM login_attempts ORDER BY username"))
}

func TestDeleteStaleLoginAttempts_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -8)
	for _, row := range []struct {
		username    string
		lastAttempt time.Time
		lockedUntil sql.NullTime
	}{
		{"stale", old, sql.NullTime{}},
		{"stale-lock-expired", old, sql.NullTime{Time: now.Add(-time.Minute), Valid: true}},
		{"stale-still-locked", old, sql.NullTime{Time: now.Add(time.Minute), Valid: true}},
		{"recent", now.AddDate(0, 0, -6), sql.NullTime{}},
	} {
		_, err := db.ExecContext(ctx, "INSERT INTO login_attempts (username, failed_count, locked_until, last_attempt) VALUES ($1, 1, $2, $3)",
			row.username, row.lockedUntil, row.lastAttempt)
		require.NoError(t, err)
	}

	require.NoError(t, database.New(db.DB).DeleteStaleLoginAttempts(ctx, database.DeleteStaleLoginAttemptsParams{Now: now, Days: 7}))

	require.Equal(t, []string{"recent", "stale-still-locked"}, columnValues(t, db.DB, "SELECT username FROM login_attempts ORDER BY username"))
}

func columnValues(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()
	var values []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		values = append(values, v)
	}
	require.NoError(t, rows.Err())
	return values
}
