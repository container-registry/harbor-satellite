package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainSealer stores secrets as they are; the test is about delivery.
type plainSealer struct{}

func (plainSealer) Seal(p []byte) ([]byte, error) { return p, nil }
func (plainSealer) Open(c []byte) ([]byte, error) { return c, nil }

// A dispatcher stopped between two runs and a new one started on the same
// database deliver every event ID exactly once: the checkpoint is durable.
func TestDispatcher_ExactlyOncePerEventIDAcrossGracefulRestart(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	var mu sync.Mutex
	seen := map[string]int{}
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev webhook.CloudEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err == nil {
			mu.Lock()
			seen[ev.ID]++
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)
	repo, err := postgres.NewWebhookRepo(db.DB)
	require.NoError(t, err)
	_, err = repo.Create(ctx, fleet.WebhookSubscription{URL: receiver.URL}, []byte("whsec-0123456789abcdef0123456789abcdef"), "admin")
	require.NoError(t, err)
	q := database.New(db.DB)
	appendEvents := func(n int) {
		t.Helper()
		for range n {
			_, err := q.AppendOutboxEvent(ctx, database.AppendOutboxEventParams{
				Kind: string(fleet.EventGroupChanged), AggregateType: "group", AggregateID: 1, Payload: []byte(`{"name":"g1"}`), LogOnly: true,
			})
			require.NoError(t, err)
		}
	}
	runDispatcher := func(want int) {
		t.Helper()
		subs, err := postgres.NewWebhookStore(db.DB)
		require.NoError(t, err)
		log, err := postgres.NewOutboxLog(db.DB)
		require.NoError(t, err)
		locker, err := postgres.NewSubscriptionLocker(ctx, db.DB)
		require.NoError(t, err)
		defer func() { require.NoError(t, locker.Close()) }()
		d, err := webhook.NewDispatcher(subs, log, locker, plainSealer{},
			webhook.NewHTTPSender(&http.Client{Timeout: 5 * time.Second}, fleettest.Clock{T: time.Now()}), webhook.DefaultDispatcherConfig())
		require.NoError(t, err)
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			_, err := d.RunOnce(ctx)
			assert.NoError(c, err)
			mu.Lock()
			defer mu.Unlock()
			assert.GreaterOrEqual(c, len(seen), want)
		}, 10*time.Second, 50*time.Millisecond)
	}

	appendEvents(3)
	runDispatcher(3)
	appendEvents(2)
	runDispatcher(5)

	mu.Lock()
	defer mu.Unlock()
	for id, n := range seen {
		require.Equal(t, 1, n, "event %s", id)
	}
}

// Retention never deletes a done event that an enabled subscription has
// not passed yet.
func TestOutboxStore_PruneKeepsRowsAboveEnabledCheckpoint(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	repo, err := postgres.NewWebhookRepo(db.DB)
	require.NoError(t, err)
	sub, err := repo.Create(ctx, fleet.WebhookSubscription{URL: "https://hooks.example/a"}, []byte("whsec-0123456789abcdef0123456789abcdef"), "admin")
	require.NoError(t, err)
	_, err = database.New(db.DB).AppendOutboxEvent(ctx, database.AppendOutboxEventParams{
		Kind: string(fleet.EventGroupChanged), AggregateType: "group", AggregateID: 1, Payload: []byte(`{"name":"g1"}`), LogOnly: true,
	})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE event_outbox SET done_at = NOW() - INTERVAL '2 hours'")
	require.NoError(t, err)
	store, err := postgres.NewOutboxStore(db.DB)
	require.NoError(t, err)

	n, err := store.Prune(ctx, time.Hour, 100)
	require.NoError(t, err)
	require.Zero(t, n, "the subscription has not received it")

	require.NoError(t, repo.Delete(ctx, sub.ID))
	n, err = store.Prune(ctx, time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

// Refresh rereads a subscription under its lock and answers no row once it
// is disabled, not yet due again or deleted.
func TestWebhookStore_RefreshAnswersOnlyADueEnabledSubscription(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	repo, err := postgres.NewWebhookRepo(db.DB)
	require.NoError(t, err)
	store, err := postgres.NewWebhookStore(db.DB)
	require.NoError(t, err)
	create := func() fleet.WebhookID {
		t.Helper()
		sub, createErr := repo.Create(ctx, fleet.WebhookSubscription{URL: "https://hooks.example/a"}, []byte("whsec-0123456789abcdef0123456789abcdef"), "admin")
		require.NoError(t, createErr)
		_, ok, refreshErr := store.Refresh(ctx, sub.ID)
		require.NoError(t, refreshErr)
		require.True(t, ok, "a new subscription is due")
		return sub.ID
	}

	disabled := create()
	_, err = db.ExecContext(ctx, "UPDATE webhook_subscriptions SET enabled = FALSE WHERE id = $1", int32(disabled))
	require.NoError(t, err)
	notDue := create()
	_, err = store.Fail(ctx, notDue, http.StatusBadGateway, "receiver down", time.Hour, 24*time.Hour)
	require.NoError(t, err)
	deleted := create()
	require.NoError(t, repo.Delete(ctx, deleted))

	for name, id := range map[string]fleet.WebhookID{"disabled": disabled, "not yet due": notDue, "deleted": deleted} {
		_, ok, refreshErr := store.Refresh(ctx, id)
		require.NoError(t, refreshErr, name)
		require.False(t, ok, name)
	}
}
