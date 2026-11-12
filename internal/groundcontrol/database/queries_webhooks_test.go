package database_test

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// position is an outbox row's (xid, id) as the queries exchange it.
type position struct {
	xid string
	id  int64
}

func appendLogOnly(t *testing.T, ctx context.Context, q *database.Queries) int64 {
	t.Helper()
	id, err := q.AppendOutboxEvent(ctx, database.AppendOutboxEventParams{
		Kind: "EdgeReplicated", AggregateType: "edge", AggregateID: 1, Payload: []byte(`{"name":"edge-01"}`), LogOnly: true,
	})
	require.NoError(t, err)
	return id
}

func positionOf(t *testing.T, db *dbtest.DB, id int64) position {
	t.Helper()
	p := position{id: id}
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT xid::TEXT FROM event_outbox WHERE id = $1", id).Scan(&p.xid))
	return p
}

func checkpointOf(t *testing.T, db *dbtest.DB, sub int32) position {
	t.Helper()
	var p position
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT last_xid::TEXT, last_id FROM webhook_checkpoints WHERE subscription_id = $1", sub).Scan(&p.xid, &p.id))
	return p
}

func readAfter(t *testing.T, ctx context.Context, q *database.Queries, after position) []int64 {
	t.Helper()
	rows, err := q.ReadOutboxAfter(ctx, database.ReadOutboxAfterParams{AfterXid: after.xid, AfterID: after.id, MaxRows: 100})
	require.NoError(t, err)
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func newSubscription(t *testing.T, ctx context.Context, q *database.Queries, url string) database.CreateWebhookSubscriptionRow {
	t.Helper()
	sub, err := q.CreateWebhookSubscription(ctx, database.CreateWebhookSubscriptionParams{
		Url: url, SecretCiphertext: []byte("sealed"), EventTypes: []string{"GroupChanged"},
	})
	require.NoError(t, err)
	return sub
}

func TestWebhookTables_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()

	sub := newSubscription(t, ctx, q, "https://hooks.example/a")
	require.True(t, sub.Enabled)
	require.Equal(t, []string{"GroupChanged"}, sub.EventTypes)
	listed, err := q.ListWebhookSubscriptions(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Zero(t, listed[0].Attempts)
	due, err := q.ListDueWebhooks(ctx, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, []byte("sealed"), due[0].SecretCiphertext)

	n, err := q.DeleteWebhookSubscription(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	n, err = q.DeleteWebhookSubscription(ctx, sub.ID)
	require.NoError(t, err)
	require.Zero(t, n)
	var checkpoints int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM webhook_checkpoints").Scan(&checkpoints))
	require.Zero(t, checkpoints, "the checkpoint goes with its subscription")
}

// T_a writes first and commits last; T_b appends and commits in between.
// A reader that took T_b's row would pass T_a's lower xid and never see it.
func TestReadOutboxAfter_XidGuardHidesInFlight(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	start := position{xid: "0", id: 0}

	ta, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() {
		if err := ta.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	aID := appendLogOnly(t, ctx, q.WithTx(ta))
	tb, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	bID := appendLogOnly(t, ctx, q.WithTx(tb))
	require.NoError(t, tb.Commit())

	require.Empty(t, readAfter(t, ctx, q, start), "T_b's row waits for T_a")

	require.NoError(t, ta.Commit())
	// xids are cluster-wide: another test's open transaction holds xmin back.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, []int64{aID, bID}, readAfterC(c, ctx, q, start), "xid order: T_a first")
	}, 10*time.Second, 50*time.Millisecond)
}

// readAfterC is readAfter for an EventuallyWithT condition.
func readAfterC(c *assert.CollectT, ctx context.Context, q *database.Queries, after position) []int64 {
	rows, err := q.ReadOutboxAfter(ctx, database.ReadOutboxAfterParams{AfterXid: after.xid, AfterID: after.id, MaxRows: 100})
	assert.NoError(c, err)
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// The text alias of xid must not decide the order: as text, 10 sorts
// before 9, and a batch would pass xid 9 for good.
func TestReadOutboxAfter_OrdersByXidNumerically(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	insert := func(xid string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, db.QueryRowContext(ctx,
			"INSERT INTO event_outbox (xid, kind, aggregate_type, aggregate_id, done_at) VALUES ($1::TEXT::XID8, 'EdgeReplicated', 'edge', 1, NOW()) RETURNING id", xid).Scan(&id))
		return id
	}
	ten := insert("10")
	nine := insert("9")

	rows, err := q.ReadOutboxAfter(ctx, database.ReadOutboxAfterParams{AfterXid: "0", AfterID: 0, MaxRows: 1})

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, nine, rows[0].ID, "xid 9 before xid 10 (id %d)", ten)
	require.Equal(t, "9", rows[0].Xid)
}

// The checkpoint is the cluster-wide xmin, so a transaction still open
// when the subscription is created is delivered once it commits. Events of
// transactions that finished before xmin are skipped; which those are
// depends on every open transaction of the server, so the test asserts
// only what the held transaction pins.
func TestCreateWebhookSubscription_StartsAtCurrentXmin(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	held, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() {
		if err := held.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	heldID := appendLogOnly(t, ctx, q.WithTx(held))
	var heldXid uint64
	require.NoError(t, held.QueryRowContext(ctx, "SELECT pg_current_xact_id()::TEXT::BIGINT").Scan(&heldXid))

	sub := newSubscription(t, ctx, q, "https://hooks.example/a")

	cp := checkpointOf(t, db, sub.ID)
	require.Zero(t, cp.id)
	cpXid, err := strconv.ParseUint(cp.xid, 10, 64)
	require.NoError(t, err)
	require.LessOrEqual(t, cpXid, heldXid, "the checkpoint is at or below every open transaction")
	require.NoError(t, held.Commit())
	after := appendLogOnly(t, ctx, q)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ids := readAfterC(c, ctx, q, cp)
		assert.Contains(c, ids, heldID, "the transaction open at creation is delivered")
		assert.Contains(c, ids, after)
	}, 10*time.Second, 50*time.Millisecond)
}

func TestAdvanceWebhookCheckpoint_NeverMovesBack(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	sub := newSubscription(t, ctx, q, "https://hooks.example/a")
	first, second := positionOf(t, db, appendLogOnly(t, ctx, q)), positionOf(t, db, appendLogOnly(t, ctx, q))
	advance := func(to position) {
		t.Helper()
		require.NoError(t, q.AdvanceWebhookCheckpoint(ctx, database.AdvanceWebhookCheckpointParams{
			LastXid: to.xid, LastID: to.id, LastStatus: sql.NullInt32{Int32: 200, Valid: true}, SubscriptionID: sub.ID,
		}))
	}

	advance(second)
	advance(first)

	require.Equal(t, second, checkpointOf(t, db, sub.ID))
}

func TestFailWebhookDelivery_DisablesAfterContinuousFailure(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	sub := newSubscription(t, ctx, q, "https://hooks.example/a")
	fail := func() database.FailWebhookDeliveryRow {
		t.Helper()
		row, err := q.FailWebhookDelivery(ctx, database.FailWebhookDeliveryParams{
			LastStatus: sql.NullInt32{Int32: 503, Valid: true}, LastError: "receiver down",
			RetryInMs: 0, SubscriptionID: sub.ID, DisableAfterMs: time.Hour.Milliseconds(),
		})
		require.NoError(t, err)
		return row
	}

	first := fail()
	require.Equal(t, database.FailWebhookDeliveryRow{Attempts: 1, Disabled: false}, first)
	_, err := db.ExecContext(ctx, "UPDATE webhook_checkpoints SET failing_since = NOW() - INTERVAL '2 hours'")
	require.NoError(t, err)
	second := fail()
	require.Equal(t, database.FailWebhookDeliveryRow{Attempts: 2, Disabled: true}, second)

	listed, err := q.ListWebhookSubscriptions(ctx)
	require.NoError(t, err)
	require.False(t, listed[0].Enabled)
	require.Contains(t, listed[0].DisabledReason.String, "receiver down")
	due, err := q.ListDueWebhooks(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, due, "a disabled subscription is never due")
}

func TestPruneDoneOutboxEvents_KeepsRowsAboveLowestEnabledCheckpoint(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	enabled := newSubscription(t, ctx, q, "https://hooks.example/enabled")
	disabled := newSubscription(t, ctx, q, "https://hooks.example/disabled")
	ids := []int64{appendLogOnly(t, ctx, q), appendLogOnly(t, ctx, q), appendLogOnly(t, ctx, q)}
	open, err := q.AppendOutboxEvent(ctx, database.AppendOutboxEventParams{Kind: "GroupChanged", AggregateType: "group", AggregateID: 1, Payload: []byte(`{}`)})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE event_outbox SET done_at = NOW() - INTERVAL '2 hours' WHERE done_at IS NOT NULL")
	require.NoError(t, err)
	first := positionOf(t, db, ids[0])
	_, err = db.ExecContext(ctx, "UPDATE webhook_checkpoints SET last_xid = $1::TEXT::XID8, last_id = $2 WHERE subscription_id = $3", first.xid, first.id, enabled.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE webhook_checkpoints SET last_xid = '0'::XID8, last_id = 0 WHERE subscription_id = $1", disabled.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE webhook_subscriptions SET enabled = FALSE WHERE id = $1", disabled.ID)
	require.NoError(t, err)
	remaining := func() []int64 {
		t.Helper()
		rows, err := db.QueryContext(ctx, "SELECT id FROM event_outbox ORDER BY id")
		require.NoError(t, err)
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var id int64
			require.NoError(t, rows.Scan(&id))
			out = append(out, id)
		}
		require.NoError(t, rows.Err())
		return out
	}
	prune := func() int64 {
		t.Helper()
		n, err := q.PruneDoneOutboxEvents(ctx, database.PruneDoneOutboxEventsParams{RetentionMs: time.Hour.Milliseconds(), MaxRows: 100})
		require.NoError(t, err)
		return n
	}

	require.Equal(t, int64(1), prune(), "only the row at the enabled checkpoint; a disabled one holds nothing")
	require.Equal(t, []int64{ids[1], ids[2], open}, remaining())

	_, err = db.ExecContext(ctx, "UPDATE webhook_subscriptions SET enabled = FALSE WHERE id = $1", enabled.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), prune())
	require.Equal(t, []int64{open}, remaining(), "an event not done is never pruned")
}
