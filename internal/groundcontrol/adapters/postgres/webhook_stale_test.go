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

// staleDue answers Due with what it listed before another worker ran.
type staleDue struct {
	*postgres.WebhookStore
	due []webhook.DueSubscription
}

func (s staleDue) Due(context.Context, int) ([]webhook.DueSubscription, error) { return s.due, nil }

// A worker that listed a subscription before another worker delivered and
// advanced it must not replay those events once it gets the lock.
func TestDispatcher_StaleDueListDoesNotRedeliver(t *testing.T) {
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
	for range 3 {
		_, err = database.New(db.DB).AppendOutboxEvent(ctx, database.AppendOutboxEventParams{
			Kind: string(fleet.EventGroupChanged), AggregateType: "group", AggregateID: 1, Payload: []byte(`{"name":"g1"}`), LogOnly: true,
		})
		require.NoError(t, err)
	}
	subs, err := postgres.NewWebhookStore(db.DB)
	require.NoError(t, err)
	log, err := postgres.NewOutboxLog(db.DB)
	require.NoError(t, err)
	dispatcher := func(store webhook.SubscriptionStore) *webhook.Dispatcher {
		locker, lockErr := postgres.NewSubscriptionLocker(ctx, db.DB)
		require.NoError(t, lockErr)
		t.Cleanup(func() { _ = locker.Close() })
		d, newErr := webhook.NewDispatcher(store, log, locker, plainSealer{},
			webhook.NewHTTPSender(&http.Client{Timeout: 5 * time.Second}, fleettest.Clock{T: time.Now()}), webhook.DefaultDispatcherConfig())
		require.NoError(t, newErr)
		return d
	}
	listed, err := subs.Due(ctx, 100)
	require.NoError(t, err)
	require.Len(t, listed, 1)

	// The outbox xmin guard is cluster wide, so transactions of tests running in
	// other databases can hide the events from a single pass.
	fresh, total := dispatcher(subs), 0
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		n, runErr := fresh.RunOnce(ctx)
		if !assert.NoError(c, runErr) {
			return
		}
		total += n
		assert.Equal(c, 3, total)
	}, 10*time.Second, 50*time.Millisecond)
	delivered, err := dispatcher(staleDue{WebhookStore: subs, due: listed}).RunOnce(ctx)
	require.NoError(t, err)

	require.Zero(t, delivered, "the stale worker replayed delivered events")
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 3)
	for id, n := range seen {
		require.Equal(t, 1, n, "event %s", id)
	}
}
