package postgres_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingOutbox = "the outbox adapters are stubs; handlers call Harbor in the request"

func appendEvents(t *testing.T, db *dbtest.DB, n int) []int64 {
	t.Helper()
	q := database.New(db.DB)
	ids := make([]int64, 0, n)
	for i := range n {
		id, err := q.AppendOutboxEvent(context.Background(), database.AppendOutboxEventParams{
			Kind:          string(fleet.EventSatelliteCreated),
			AggregateType: string(fleet.AggregateSatellite),
			AggregateID:   int64(i + 1),
			Payload:       json.RawMessage(`{}`),
		})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return ids
}

func TestOutboxSink_AppendsOnlyWithItsTransaction(t *testing.T) {
	pending.Plan(t, "2.1", pendingOutbox)
	db := dbtest.New(t)
	ctx := context.Background()
	event := fleet.Event{
		Kind:      fleet.EventSatelliteDeleted,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7},
		Payload:   fleet.SatelliteDeletedPayload{Name: "edge-01", RobotHarborIDs: []int64{42}, StateRef: fleet.ArtifactRef{Registry: "http://harbor.example", Repository: "satellite/satellite-state/edge-01/state", Tag: "latest"}},
	}
	appendIn := func(commit bool) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		sink, err := postgres.NewOutboxSink(database.New(db.DB).WithTx(tx))
		require.NoError(t, err)
		require.NoError(t, sink.Append(ctx, event))
		if commit {
			require.NoError(t, tx.Commit())
		} else {
			require.NoError(t, tx.Rollback())
		}
	}

	appendIn(false)
	pendingCount, err := database.New(db.DB).CountPendingOutboxEvents(ctx)
	require.NoError(t, err)
	require.Zero(t, pendingCount, "a rolled back change leaves no event")

	appendIn(true)
	var kind, aggregateType string
	var aggregateID int64
	var payload json.RawMessage
	require.NoError(t, db.QueryRowContext(ctx, "SELECT kind, aggregate_type, aggregate_id, payload FROM event_outbox").Scan(&kind, &aggregateType, &aggregateID, &payload))
	require.Equal(t, string(fleet.EventSatelliteDeleted), kind)
	require.Equal(t, string(fleet.AggregateSatellite), aggregateType)
	require.EqualValues(t, 7, aggregateID)
	require.JSONEq(t, `{"name":"edge-01","robot_harbor_ids":[42],"state_ref":{"registry":"http://harbor.example","repository":"satellite/satellite-state/edge-01/state","tag":"latest"}}`, string(payload))
}

func TestOutboxStore_ClaimFailCompleteStuck(t *testing.T) {
	pending.Plan(t, "2.1", pendingOutbox)
	db := dbtest.New(t)
	ctx := context.Background()
	ids := appendEvents(t, db, 3)
	store, err := postgres.NewOutboxStore(db.DB)
	require.NoError(t, err)

	claimed, err := store.Claim(ctx, 10, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 3)
	for i, e := range claimed {
		require.Equal(t, ids[i], e.ID, "ordered by ID")
		require.Equal(t, fleet.EventSatelliteCreated, e.Kind)
		require.Equal(t, fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(i + 1)}, e.Aggregate)
		require.EqualValues(t, 1, e.Attempts)
	}

	require.NoError(t, store.Fail(ctx, ids[0], "registry down", -time.Second))
	require.NoError(t, store.Complete(ctx, ids[1:]))
	again, err := store.Claim(ctx, 10, 2, -time.Second)
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.NoError(t, store.Fail(ctx, ids[0], "still down", -time.Second))

	stuck, err := store.Stuck(ctx, 2, 10)
	require.NoError(t, err)
	require.Len(t, stuck, 1)
	require.Equal(t, ids[0], stuck[0].ID)
	require.Equal(t, "still down", stuck[0].LastError)
	pendingCount, err := store.PendingCount(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, pendingCount)
}

func TestDesiredStateStore_RecordPublishGet(t *testing.T) {
	pending.Plan(t, "2.1", pendingOutbox)
	db := dbtest.New(t)
	ctx := context.Background()
	store, err := postgres.NewDesiredStateStore(db.DB)
	require.NoError(t, err)
	agg := fleet.Aggregate{Type: fleet.AggregateGroup, ID: 1}

	_, err = store.Get(ctx, agg)
	require.ErrorIs(t, err, fleet.ErrNotFound)
	version, publish, err := store.Record(ctx, agg, json.RawMessage(`{"group":"g1"}`), "sha256:a")
	require.NoError(t, err)
	require.EqualValues(t, 1, version)
	require.True(t, publish)
	require.NoError(t, store.MarkPublished(ctx, agg, "sha256:a", "sha256:manifest"))
	_, publish, err = store.Record(ctx, agg, json.RawMessage(`{"group":"g1"}`), "sha256:a")
	require.NoError(t, err)
	require.False(t, publish)

	got, err := store.Get(ctx, agg)
	require.NoError(t, err)
	require.Equal(t, fleet.Digest("sha256:a"), got.PublishedDigest)
	require.Equal(t, fleet.Digest("sha256:manifest"), got.PublishedArtifactDigest)
	require.NoError(t, store.ClearPublished(ctx, agg))
	_, publish, err = store.Record(ctx, agg, json.RawMessage(`{"group":"g1"}`), "sha256:a")
	require.NoError(t, err)
	require.True(t, publish, "cleared rows publish again")
	require.JSONEq(t, `{"group":"g1"}`, string(got.Document))
	require.NoError(t, store.Delete(ctx, agg))
	_, err = store.Get(ctx, agg)
	require.ErrorIs(t, err, fleet.ErrNotFound)
}

// A publisher killed mid-batch leaves its claimed events leased; another
// publisher takes them over once the lease ends. Delivery is at least
// once: the second may handle again what the first finished but had not
// completed yet, which level-triggered handlers make harmless.
func TestPublisher_KillMidBatchConverges(t *testing.T) {
	pending.Plan(t, "2.3", pendingOutbox)
	db := dbtest.New(t)
	ids := appendEvents(t, db, 5)
	store, err := postgres.NewOutboxStore(db.DB)
	require.NoError(t, err)
	cfg := async.DefaultPublisherConfig()
	cfg.Concurrency = 1
	cfg.Lease = 300 * time.Millisecond
	var mu sync.Mutex
	handledBy := map[string][]int64{}
	record := func(who string, id int64) {
		mu.Lock()
		defer mu.Unlock()
		handledBy[who] = append(handledBy[who], id)
	}

	killed, kill := context.WithCancel(context.Background())
	defer kill()
	newDeps := func(h async.Handler) async.PublisherDeps {
		locker, err := postgres.NewAggregateLocker(context.Background(), db.DB)
		require.NoError(t, err)
		t.Cleanup(func() { _ = locker.Close() })
		return async.PublisherDeps{Store: store, Handler: h, Locker: locker, Registry: reachable{}}
	}
	first, err := async.NewPublisher(newDeps(async.HandlerFunc(func(ctx context.Context, e async.OutboxEvent) error {
		record("first", e.ID)
		if e.ID == ids[2] {
			kill()
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})), cfg)
	require.NoError(t, err)
	if _, err := first.RunOnce(killed); err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}

	second, err := async.NewPublisher(newDeps(async.HandlerFunc(func(_ context.Context, e async.OutboxEvent) error {
		record("second", e.ID)
		return nil
	})), cfg)
	require.NoError(t, err)
	n, err := second.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, n, "the killed publisher's events are leased")

	deadline := time.Now().Add(10 * time.Second)
	for {
		pendingCount, err := store.PendingCount(context.Background())
		require.NoError(t, err)
		if pendingCount == 0 {
			break
		}
		require.True(t, time.Now().Before(deadline), "outbox did not drain")
		time.Sleep(50 * time.Millisecond)
		_, err = second.RunOnce(context.Background())
		require.NoError(t, err)
	}

	require.Equal(t, ids[:3], handledBy["first"])
	handled := map[int64]bool{}
	for _, who := range []string{"first", "second"} {
		for _, id := range handledBy[who] {
			handled[id] = true
		}
	}
	require.Len(t, handled, len(ids), "every event is handled at least once")
	require.Contains(t, handledBy["second"], ids[2], "the cut-short event is handled again")
}

type reachable struct{}

func (reachable) Reachable() bool { return true }

func TestOutboxStore_ReleaseAndRearm(t *testing.T) {
	pending.Plan(t, "2.1", pendingOutbox)
	db := dbtest.New(t)
	ctx := context.Background()
	ids := appendEvents(t, db, 2)
	store, err := postgres.NewOutboxStore(db.DB)
	require.NoError(t, err)
	past := -time.Second

	for range 3 {
		claimed, err := store.Claim(ctx, 1, 2, past)
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		require.Equal(t, ids[0], claimed[0].ID)
		require.NoError(t, store.Release(ctx, ids[0], "registry unavailable", past))
	}
	for range 2 {
		_, err := store.Claim(ctx, 10, 2, past)
		require.NoError(t, err)
	}
	stuck, err := store.Stuck(ctx, 2, 10)
	require.NoError(t, err)
	require.Len(t, stuck, 2, "both ran out of attempts after the releases stopped")

	n, err := store.Rearm(ctx, 2)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
	stuck, err = store.Stuck(ctx, 1, 10)
	require.NoError(t, err)
	require.Empty(t, stuck)
}

func TestOutboxSink_FansOutToGroupMembersAndKeepsLogOnlyDone(t *testing.T) {
	pending.Plan(t, "2.1", pendingOutbox)
	db := dbtest.New(t)
	ctx := context.Background()
	q := database.New(db.DB)
	group, err := q.CreateGroup(ctx, database.CreateGroupParams{GroupName: "g1", RegistryUrl: "http://harbor.example", Projects: []string{"library"}})
	require.NoError(t, err)
	for _, name := range []string{"edge-01", "edge-02"} {
		sat, err := q.CreateSatellite(ctx, name)
		require.NoError(t, err)
		require.NoError(t, q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: sat.ID, GroupID: group.ID}))
	}
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	sink, err := postgres.NewOutboxSink(q.WithTx(tx))
	require.NoError(t, err)

	require.NoError(t, sink.AppendToGroupMembers(ctx, fleet.GroupID(group.ID), fleet.EventSatelliteChanged))
	require.NoError(t, tx.Commit())

	pendingCount, err := q.CountPendingOutboxEvents(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, pendingCount)
}

// Two workers on one aggregate: the second waits for the first (A86).
func TestAggregateLocker_ExcludesOtherWorkers(t *testing.T) {
	pending.Plan(t, "2.3", pendingOutbox)
	db := dbtest.New(t)
	ctx := context.Background()
	newLocker := func() *postgres.AggregateLocker {
		l, err := postgres.NewAggregateLocker(ctx, db.DB)
		require.NoError(t, err)
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	first, second := newLocker(), newLocker()
	satellite := fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7}

	unlock, ok, err := first.TryLock(ctx, satellite)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = second.TryLock(ctx, satellite)
	require.NoError(t, err)
	require.False(t, ok, "the other worker holds satellite 7")
	unlockGroup, ok, err := second.TryLock(ctx, fleet.Aggregate{Type: fleet.AggregateGroup, ID: 7})
	require.NoError(t, err)
	require.True(t, ok, "group 7 is another aggregate")
	unlockGroup()

	unlock()
	unlock2, ok, err := second.TryLock(ctx, satellite)
	require.NoError(t, err)
	require.True(t, ok)
	unlock2()
	var open int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state = 'idle in transaction'").Scan(&open))
	require.Zero(t, open, "the lock holds no transaction")
}
