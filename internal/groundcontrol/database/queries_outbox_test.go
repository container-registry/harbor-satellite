package database_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func appendEvent(t *testing.T, q *database.Queries, kind string, aggregateID int64) int64 {
	t.Helper()
	id, err := q.AppendOutboxEvent(context.Background(), database.AppendOutboxEventParams{
		Kind:          kind,
		AggregateType: "satellite",
		AggregateID:   aggregateID,
		Payload:       json.RawMessage(`{"robot_id":42}`),
	})
	require.NoError(t, err)
	return id
}

// claim leases for lease; a negative lease leaves the events due at once.
func claim(t *testing.T, q *database.Queries, batch, maxAttempts int32, lease time.Duration) map[int64]database.ClaimOutboxEventsRow {
	t.Helper()
	rows, err := q.ClaimOutboxEvents(context.Background(), database.ClaimOutboxEventsParams{LeaseMs: lease.Milliseconds(), MaxAttempts: maxAttempts, BatchSize: batch})
	require.NoError(t, err)
	out := map[int64]database.ClaimOutboxEventsRow{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

func TestOutboxQueries_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	lease := time.Minute
	e1 := appendEvent(t, q, "SatelliteCreated", 7)
	e2 := appendEvent(t, q, "SatelliteDeleted", 8)
	e3 := appendEvent(t, q, "GroupChanged", 1)
	pending, err := q.CountPendingOutboxEvents(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, pending)

	first := claim(t, q, 2, 10, lease)
	require.Len(t, first, 2, "batch size bounds a claim")
	require.Contains(t, first, e1)
	require.Contains(t, first, e2)
	require.EqualValues(t, 1, first[e1].Attempts, "a claim counts as an attempt")
	require.JSONEq(t, `{"robot_id":42}`, string(first[e1].Payload))
	require.Equal(t, "SatelliteCreated", first[e1].Kind)
	require.EqualValues(t, 7, first[e1].AggregateID)

	second := claim(t, q, 10, 10, lease)
	require.Equal(t, []int64{e3}, keys(second), "leased events are not claimed again")
	require.Empty(t, claim(t, q, 10, 10, lease))

	require.NoError(t, q.FailOutboxEvent(ctx, database.FailOutboxEventParams{ID: e1, LastError: "registry down", RetryInMs: -1000}))
	retried := claim(t, q, 10, 10, lease)
	require.Equal(t, []int64{e1}, keys(retried), "a failed event is due at its next attempt")
	require.EqualValues(t, 2, retried[e1].Attempts)
	stuck, err := q.ListStuckOutboxEvents(ctx, database.ListStuckOutboxEventsParams{MinAttempts: 2, MaxRows: 10})
	require.NoError(t, err)
	require.Len(t, stuck, 1)
	require.Equal(t, e1, stuck[0].ID)
	require.Equal(t, "registry down", stuck[0].LastError.String)

	require.NoError(t, q.CompleteOutboxEvents(ctx, []int64{e1, e2, e3}))
	pending, err = q.CountPendingOutboxEvents(ctx)
	require.NoError(t, err)
	require.Zero(t, pending)
	require.NoError(t, q.FailOutboxEvent(ctx, database.FailOutboxEventParams{ID: e2, LastError: "late", RetryInMs: -1000}))
	require.Empty(t, claim(t, q, 10, 10, lease), "done events are never claimed, even after a late failure")
	all, err := q.ListStuckOutboxEvents(ctx, database.ListStuckOutboxEventsParams{MinAttempts: 0, MaxRows: 10})
	require.NoError(t, err)
	require.Empty(t, all)
}

func TestClaimOutboxEvents_StopsAtMaxAttempts(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	id := appendEvent(t, q, "SatelliteCreated", 7)
	past := -time.Second

	for range 2 {
		require.Len(t, claim(t, q, 10, 2, past), 1)
	}

	require.Empty(t, claim(t, q, 10, 2, past), "an event claimed max_attempts times stays failed")
	stuck, err := q.ListStuckOutboxEvents(ctx, database.ListStuckOutboxEventsParams{MinAttempts: 2, MaxRows: 10})
	require.NoError(t, err)
	require.Len(t, stuck, 1)
	require.Equal(t, id, stuck[0].ID)
}

// Two workers claiming at the same time get disjoint events.
func TestClaimOutboxEvents_SkipsLockedRows(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	for i := range 4 {
		appendEvent(t, q, "SatelliteCreated", int64(i))
	}
	lease := time.Minute

	tx1, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	first := claim(t, q.WithTx(tx1), 1, 10, lease)
	require.Len(t, first, 1)

	tx2, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	second := claim(t, q.WithTx(tx2), 10, 10, lease)
	require.Len(t, second, 3, "the row tx1 holds is skipped, not waited for")
	for id := range first {
		require.NotContains(t, second, id)
	}
	require.NoError(t, tx2.Commit())
	require.NoError(t, tx1.Commit())
}

// Events of one transaction share its xid; webhooks order by it (plan item
// 9.1).
func TestAppendOutboxEvent_RecordsTheTransactionID(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	a := appendEvent(t, q.WithTx(tx), "GroupChanged", 1)
	b := appendEvent(t, q.WithTx(tx), "GroupChanged", 2)
	require.NoError(t, tx.Commit())
	c := appendEvent(t, q, "GroupChanged", 3)

	xid := func(id int64) string {
		var x string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT xid::TEXT FROM event_outbox WHERE id = $1", id).Scan(&x))
		return x
	}
	require.Equal(t, xid(a), xid(b))
	require.NotEqual(t, xid(a), xid(c))
}

func keys[V any](m map[int64]V) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestAppendOutboxEvent_LogOnlyIsAlreadyDone(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()

	_, err := q.AppendOutboxEvent(ctx, database.AppendOutboxEventParams{Kind: "EdgePullThrough", AggregateType: "edge", AggregateID: 7, Payload: json.RawMessage(`{}`), LogOnly: true})
	require.NoError(t, err)

	pending, err := q.CountPendingOutboxEvents(ctx)
	require.NoError(t, err)
	require.Zero(t, pending)
	require.Empty(t, claim(t, q, 10, 10, time.Minute))
	var events int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM event_outbox WHERE done_at IS NOT NULL").Scan(&events))
	require.Equal(t, 1, events, "readers of the log still see it")
}

func TestReleaseOutboxEvent_GivesTheAttemptBack(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	id := appendEvent(t, q, "SatelliteCreated", 7)
	past := -time.Second

	for range 5 {
		require.Len(t, claim(t, q, 10, 2, past), 1, "released claims never reach max_attempts")
		require.NoError(t, q.ReleaseOutboxEvent(ctx, database.ReleaseOutboxEventParams{ID: id, LastError: "registry unavailable", RetryInMs: past.Milliseconds()}))
	}

	stuck, err := q.ListStuckOutboxEvents(ctx, database.ListStuckOutboxEventsParams{MinAttempts: 0, MaxRows: 10})
	require.NoError(t, err)
	require.Len(t, stuck, 1)
	require.Zero(t, stuck[0].Attempts)
	require.Equal(t, "registry unavailable", stuck[0].LastError.String)
}

func TestRearmOutboxEvents_MakesDeadEventsDue(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	dead := appendEvent(t, q, "SatelliteCreated", 7)
	past := -time.Second
	for range 2 {
		require.Len(t, claim(t, q, 1, 2, past), 1)
	}
	alive := appendEvent(t, q, "SatelliteCreated", 8)
	require.Equal(t, []int64{alive}, keys(claim(t, q, 10, 2, time.Minute)), "leased, not dead")

	n, err := q.RearmOutboxEvents(ctx, 2)

	require.NoError(t, err)
	require.EqualValues(t, 1, n, "only the event that ran out of attempts")
	require.Equal(t, []int64{dead}, keys(claim(t, q, 10, 2, time.Minute)))
}

func TestAppendGroupMemberEvents_OneStatementForAllMembers(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	group, err := q.CreateGroup(ctx, database.CreateGroupParams{GroupName: "g1", RegistryUrl: "http://harbor.example", Projects: []string{"library"}})
	require.NoError(t, err)
	for i := range 25 {
		sat, err := q.CreateSatellite(ctx, fmt.Sprintf("edge-%02d", i))
		require.NoError(t, err)
		require.NoError(t, q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: sat.ID, GroupID: group.ID}))
	}
	_, err = q.CreateSatellite(ctx, "not-a-member")
	require.NoError(t, err)

	var n int64
	stmts := db.MeasureStatements(t, func() {
		n, err = q.AppendGroupMemberEvents(ctx, database.AppendGroupMemberEventsParams{Kind: "SatelliteChanged", GroupID: group.ID})
	})

	require.NoError(t, err)
	require.EqualValues(t, 25, n)
	require.EqualValues(t, 1, stmts.Total, "%s", stmts)
	var named int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT count(*) FROM event_outbox o JOIN satellites s ON s.id = o.aggregate_id WHERE o.aggregate_type = 'satellite' AND o.payload->>'name' = s.name").Scan(&named))
	require.Equal(t, 25, named, "each event names its satellite")
}

// Session-level locks on dedicated connections: what AggregateLocker uses.
func TestAggregateLocks_ExcludeOtherSessions(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	conn := func() *database.Queries {
		t.Helper()
		c, err := db.Conn(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		return database.New(c)
	}
	first, second := conn(), conn()
	lock := func(q *database.Queries, namespace, id int32) bool {
		t.Helper()
		ok, err := q.TryAggregateLock(ctx, database.TryAggregateLockParams{Namespace: namespace, ID: id})
		require.NoError(t, err)
		return ok
	}

	require.True(t, lock(first, 1, 7))
	require.False(t, lock(second, 1, 7), "another session holds it")
	require.True(t, lock(second, 2, 7), "namespaces keep aggregate types apart")
	released, err := first.AggregateUnlock(ctx, database.AggregateUnlockParams{Namespace: 1, ID: 7})
	require.NoError(t, err)
	require.True(t, released)
	require.True(t, lock(second, 1, 7))
}
