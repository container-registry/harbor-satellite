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
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
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
	pending.Plan(t, "9.1", "Ground Control delivers no events; there are no webhooks")
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
