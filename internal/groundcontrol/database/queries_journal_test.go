package database_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type edgeEvent struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

func edgeEvents(t *testing.T, events ...edgeEvent) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(events)
	require.NoError(t, err)
	return data
}

// outboxEdgeEvents returns the kinds of the satellite's edge events in id
// order and fails if any is not already done.
func outboxEdgeEvents(t *testing.T, db *dbtest.DB, satellite int32) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		"SELECT kind, done_at IS NOT NULL FROM event_outbox WHERE aggregate_type = 'edge' AND aggregate_id = $1 ORDER BY id", satellite)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var kinds []string
	for rows.Next() {
		var kind string
		var done bool
		require.NoError(t, rows.Scan(&kind, &done))
		require.True(t, done, "%s is log-only", kind)
		kinds = append(kinds, kind)
	}
	require.NoError(t, rows.Err())
	return kinds
}

func TestSatelliteJournal_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	sat, err := q.CreateSatellite(ctx, "sat1")
	require.NoError(t, err)
	const epoch, reinstalled int64 = 7, 8
	open := func(epoch int64) database.OpenJournalCursorRow {
		t.Helper()
		c, err := q.OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: epoch})
		require.NoError(t, err)
		return c
	}
	cursor := func(epoch, acked int64) database.OpenJournalCursorRow {
		return database.OpenJournalCursorRow{Epoch: epoch, AckedSeq: acked}
	}
	fresh := func(epoch int64) database.OpenJournalCursorRow {
		return database.OpenJournalCursorRow{Epoch: epoch, Fresh: true}
	}
	advance := func(epoch, acked int64, events json.RawMessage) database.AdvanceJournalCursorRow {
		t.Helper()
		r, err := q.AdvanceJournalCursor(ctx, database.AdvanceJournalCursorParams{SatelliteID: sat.ID, Epoch: epoch, AckedSeq: acked, Events: events})
		require.NoError(t, err)
		return r
	}
	moved := func(events int64) database.AdvanceJournalCursorRow {
		return database.AdvanceJournalCursorRow{Moved: 1, Emitted: events}
	}

	require.Equal(t, fresh(epoch), open(epoch), "new cursor")
	require.Equal(t, cursor(epoch, 0), open(epoch), "the same epoch again is not fresh")
	require.Equal(t, moved(3), advance(epoch, 3, edgeEvents(t,
		edgeEvent{Kind: "EdgeJournalGap", Payload: json.RawMessage(`{"from": 1, "through": 1}`)},
		edgeEvent{Kind: "EdgeReplicated", Payload: json.RawMessage(`{"seq": 2}`)},
		edgeEvent{Kind: "EdgeDeleted", Payload: json.RawMessage(`{"seq": 3}`)},
	)))
	require.Equal(t, []string{"EdgeJournalGap", "EdgeReplicated", "EdgeDeleted"}, outboxEdgeEvents(t, db, sat.ID), "in the given order")
	var payload json.RawMessage
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT payload FROM event_outbox WHERE aggregate_type = 'edge' AND kind = 'EdgeDeleted'").Scan(&payload))
	require.JSONEq(t, `{"seq": 3}`, string(payload))
	require.Equal(t, cursor(epoch, 3), open(epoch))

	stay := database.AdvanceJournalCursorRow{}
	require.Equal(t, stay, advance(epoch, 3, edgeEvents(t, edgeEvent{Kind: "EdgeReplicated", Payload: json.RawMessage(`{}`)})),
		"a cursor that does not move emits nothing")
	require.Equal(t, stay, advance(epoch, 2, edgeEvents(t, edgeEvent{Kind: "EdgeReplicated", Payload: json.RawMessage(`{}`)})),
		"the cursor never moves back")
	require.Equal(t, stay, advance(reinstalled, 9, edgeEvents(t, edgeEvent{Kind: "EdgeReplicated", Payload: json.RawMessage(`{}`)})),
		"an ack of another epoch does not move the cursor")
	require.Len(t, outboxEdgeEvents(t, db, sat.ID), 3)
	require.Equal(t, cursor(epoch, 3), open(epoch))

	require.Equal(t, fresh(reinstalled), open(reinstalled), "a newer epoch resets the cursor")
	require.Equal(t, moved(1), advance(reinstalled, 1, edgeEvents(t, edgeEvent{Kind: "EdgePullThrough", Payload: json.RawMessage(`{}`)})))
	require.Equal(t, cursor(reinstalled, 1), open(epoch), "a delayed upload of an older epoch leaves the cursor")

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	})
	c, err := q.WithTx(tx).OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: epoch})
	require.NoError(t, err)
	require.Equal(t, cursor(reinstalled, 1), c)
	_, err = db.ExecContext(ctx, "SELECT 1 FROM satellite_journal_cursor WHERE satellite_id = $1 FOR UPDATE NOWAIT", sat.ID)
	var pqErr *pq.Error
	require.ErrorAs(t, err, &pqErr, "an older epoch still locks the cursor until the transaction ends")
	require.Equal(t, "55P03", string(pqErr.Code), "lock_not_available")
	require.NoError(t, tx.Rollback())

	_, err = db.ExecContext(ctx, "DELETE FROM satellites WHERE id = $1", sat.ID)
	require.NoError(t, err)
	var left int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM satellite_journal_cursor").Scan(&left))
	require.Zero(t, left, "deleting the satellite deletes its cursor")
}

// Two first uploads of one satellite race to create its cursor; the one
// that waits must still get the row (review MINOR 1).
func TestOpenJournalCursor_ConcurrentCreateReturnsTheRow(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	sat, err := q.CreateSatellite(ctx, "sat1")
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	})
	_, err = q.WithTx(tx).OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: 200})
	require.NoError(t, err)

	type result struct {
		row database.OpenJournalCursorRow
		err error
	}
	waiting := make(chan result, 1)
	go func() {
		row, err := q.OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: 100})
		waiting <- result{row, err}
	}()
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, tx.Commit())

	got := <-waiting
	require.NoError(t, got.err)
	require.Equal(t, int64(200), got.row.Epoch)
	require.Zero(t, got.row.AckedSeq)
}

// fresh must be judged against the epoch committed while the upload waited
// for the lock, not against its statement's snapshot.
func TestOpenJournalCursor_FreshSeesTheLatestCommittedEpoch(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	sat, err := q.CreateSatellite(ctx, "sat1")
	require.NoError(t, err)
	_, err = q.OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: 200})
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	})
	reset, err := q.WithTx(tx).OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: 300})
	require.NoError(t, err)
	require.True(t, reset.Fresh)

	type result struct {
		row database.OpenJournalCursorRow
		err error
	}
	waiting := make(chan result, 1)
	go func() {
		row, err := q.OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: 300})
		waiting <- result{row, err}
	}()
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, tx.Commit())

	got := <-waiting
	require.NoError(t, got.err)
	require.Equal(t, database.OpenJournalCursorRow{Epoch: 300}, got.row, "the second upload of epoch 300 finds it open")
}
