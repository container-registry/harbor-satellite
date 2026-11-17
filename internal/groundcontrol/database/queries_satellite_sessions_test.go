package database_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func sessionHashes(t *testing.T, db *dbtest.DB, satellite int32) [][]byte {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "SELECT token_hash FROM satellite_sessions WHERE satellite_id = $1 ORDER BY id", satellite)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var out [][]byte
	for rows.Next() {
		var h []byte
		require.NoError(t, rows.Scan(&h))
		out = append(out, h)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestSatelliteSessions_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	sat, err := q.CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	other, err := q.CreateSatellite(ctx, "edge-02")
	require.NoError(t, err)
	expiry := sql.NullTime{Time: now.Add(30 * 24 * time.Hour), Valid: true}
	_, err = q.AddRobotAccount(ctx, database.AddRobotAccountParams{
		RobotName: "robot$edge-01", RobotSecretHash: "hash", RobotID: "42", SatelliteID: sat.ID, RobotExpiry: expiry,
	})
	require.NoError(t, err)

	t.Run("a satellite without a session gets one only once", func(t *testing.T) {
		issue := func(token string) int64 {
			t.Helper()
			n, err := q.IssueFirstSatelliteSession(ctx, database.IssueFirstSatelliteSessionParams{
				SatelliteID: other.ID, TokenHash: tokenHash(token), ExpiresAt: now.Add(time.Hour), Now: now,
			})
			require.NoError(t, err)
			return n
		}
		require.EqualValues(t, 1, issue("first"))
		require.EqualValues(t, 0, issue("second"), "another replica issued one first")
		require.Equal(t, [][]byte{tokenHash("first")}, sessionHashes(t, db, other.ID))
	})

	t.Run("issue replaces every session of the satellite", func(t *testing.T) {
		for _, token := range []string{"ztr-1", "ztr-2"} {
			require.NoError(t, q.IssueSatelliteSession(ctx, database.IssueSatelliteSessionParams{
				SatelliteID: sat.ID, TokenHash: tokenHash(token), ExpiresAt: now.Add(time.Hour),
			}))
		}
		require.Equal(t, [][]byte{tokenHash("ztr-2")}, sessionHashes(t, db, sat.ID))
		require.Len(t, sessionHashes(t, db, other.ID), 1, "other satellites keep theirs")
	})

	t.Run("lookups return the auth context", func(t *testing.T) {
		byHash, err := q.GetSatelliteAuthByTokenHash(ctx, tokenHash("ztr-2"))
		require.NoError(t, err)
		require.Equal(t, sat.ID, byHash.SatelliteID)
		require.Equal(t, "edge-01", byHash.SatelliteName)
		require.Equal(t, "pending", byHash.State, "no satellite_view row yet")
		require.Empty(t, byHash.InventoryDigest)
		require.True(t, byHash.RobotExpiry.Valid)
		require.WithinDuration(t, expiry.Time, byHash.RobotExpiry.Time, time.Second)
		require.WithinDuration(t, now.Add(time.Hour), byHash.ExpiresAt, time.Second)

		byName, err := q.GetSatelliteAuthByName(ctx, database.GetSatelliteAuthByNameParams{Name: "edge-01", Now: now})
		require.NoError(t, err)
		require.Equal(t, sat.ID, byName.SatelliteID)
		require.Equal(t, "pending", byName.State)
		require.True(t, byName.HasSession)
		later, err := q.GetSatelliteAuthByName(ctx, database.GetSatelliteAuthByNameParams{Name: "edge-01", Now: now.Add(2 * time.Hour)})
		require.NoError(t, err)
		require.False(t, later.HasSession, "an expired session is no session")

		_, err = q.GetSatelliteAuthByTokenHash(ctx, tokenHash("unknown"))
		require.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("one successor per session", func(t *testing.T) {
		current, err := q.GetSatelliteAuthByTokenHash(ctx, tokenHash("ztr-2"))
		require.NoError(t, err)
		for _, token := range []string{"renewed-1", "renewed-2"} {
			require.NoError(t, q.RenewSatelliteSession(ctx, database.RenewSatelliteSessionParams{
				SatelliteID: sat.ID, TokenHash: tokenHash(token), ExpiresAt: now.Add(48 * time.Hour),
				RenewedFrom: sql.NullInt64{Int64: current.SessionID, Valid: true},
			}))
		}
		require.Equal(t, [][]byte{tokenHash("ztr-2"), tokenHash("renewed-2")}, sessionHashes(t, db, sat.ID),
			"the second renewal replaces the first successor")
		_, err = q.GetSatelliteAuthByTokenHash(ctx, tokenHash("renewed-1"))
		require.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("renewing a session deleted meanwhile violates the foreign key", func(t *testing.T) {
		err := q.RenewSatelliteSession(ctx, database.RenewSatelliteSessionParams{
			SatelliteID: sat.ID, TokenHash: tokenHash("orphan"), ExpiresAt: now.Add(48 * time.Hour),
			RenewedFrom: sql.NullInt64{Int64: 1 << 40, Valid: true},
		})
		var pqErr *pq.Error
		require.ErrorAs(t, err, &pqErr)
		require.Equal(t, "23503", string(pqErr.Code), "the adapter maps it to fleet.ErrSessionInvalid")
	})

	t.Run("expired sessions are deleted with the given now", func(t *testing.T) {
		n, err := q.DeleteExpiredSatelliteSessions(ctx, now.Add(2*time.Hour))
		require.NoError(t, err)
		require.EqualValues(t, 2, n, "ztr-2 and first expired, renewed-2 has not")
		require.Equal(t, [][]byte{tokenHash("renewed-2")}, sessionHashes(t, db, sat.ID))
		require.NoError(t, q.DeleteSatelliteSessions(ctx, sat.ID))
		require.Empty(t, sessionHashes(t, db, sat.ID))
	})

	t.Run("only a SHA-256 is stored", func(t *testing.T) {
		err := q.IssueSatelliteSession(ctx, database.IssueSatelliteSessionParams{
			SatelliteID: sat.ID, TokenHash: make([]byte, 31), ExpiresAt: now.Add(time.Hour),
		})
		require.ErrorContains(t, err, "satellite_sessions_token_hash_check")
	})

	t.Run("deleting the satellite deletes its sessions", func(t *testing.T) {
		require.NoError(t, q.IssueSatelliteSession(ctx, database.IssueSatelliteSessionParams{
			SatelliteID: sat.ID, TokenHash: tokenHash("last"), ExpiresAt: now.Add(time.Hour),
		}))
		_, err := db.ExecContext(ctx, "DELETE FROM satellites WHERE id = $1", sat.ID)
		require.NoError(t, err)
		require.Empty(t, sessionHashes(t, db, sat.ID))
	})
}

// Two replicas migrate the same robot-Basic satellite at once.
func TestIssueFirstSatelliteSession_ConcurrentReplicas(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	sat, err := q.CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	issueFirst := func(qq *database.Queries, token string) (int64, error) {
		return qq.IssueFirstSatelliteSession(ctx, database.IssueFirstSatelliteSessionParams{
			SatelliteID: sat.ID, TokenHash: tokenHash(token), ExpiresAt: now.Add(time.Hour), Now: now,
		})
	}
	lockAndIssue := func(tx *sql.Tx, token string) (int64, error) {
		qt := q.WithTx(tx)
		if err := qt.LockSatelliteSessions(ctx, database.LockSatelliteSessionsParams{Namespace: advisorylock.SatelliteSessions, SatelliteID: sat.ID}); err != nil {
			return 0, err
		}
		return issueFirst(qt, token)
	}

	t.Run("without the lock both issue", func(t *testing.T) {
		first, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		n, err := issueFirst(q.WithTx(first), "a")
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
		n, err = issueFirst(q, "b")
		require.NoError(t, err)
		require.EqualValues(t, 1, n, "READ COMMITTED: the open insert is invisible")
		require.NoError(t, first.Rollback())
		require.NoError(t, q.DeleteSatelliteSessions(ctx, sat.ID))
	})

	t.Run("behind the lock the second sees the first", func(t *testing.T) {
		first, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		n, err := lockAndIssue(first, "first")
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		type result struct {
			n   int64
			err error
		}
		second := make(chan result, 1)
		go func() {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				second <- result{err: err}
				return
			}
			n, err := lockAndIssue(tx, "second")
			if err != nil {
				second <- result{err: errors.Join(err, tx.Rollback())}
				return
			}
			second <- result{n: n, err: tx.Commit()}
		}()
		require.Eventually(t, func() bool {
			var waiting int
			err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
				AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&waiting)
			return err == nil && waiting == 1
		}, 10*time.Second, 10*time.Millisecond, "the second replica waits for the lock")
		require.NoError(t, first.Commit())

		got := <-second
		require.NoError(t, got.err)
		require.Zero(t, got.n)
		require.Equal(t, [][]byte{tokenHash("first")}, sessionHashes(t, db, sat.ID))
	})
}
