package async_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// pruneOutbox holds back rows while held is true, as an enabled failing
// subscription's checkpoint does until FailWebhookDelivery disables it.
type pruneOutbox struct {
	*fakeOutbox
	mu     sync.Mutex
	held   bool
	rows   int64
	prunes int
}

func (o *pruneOutbox) Prune(_ context.Context, _ time.Duration, limit int) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.prunes++
	if o.held {
		return 0, nil
	}
	n := min(o.rows, int64(limit))
	o.rows -= n
	return n, nil
}

func TestOutboxPruner_FailingSubscriptionStopsHoldingRowsOnceDisabled(t *testing.T) {
	pending.Plan(t, "9.1", "done outbox events are never pruned")
	store := &pruneOutbox{fakeOutbox: newFakeOutbox(), held: true, rows: 250}
	p, err := async.NewOutboxPruner(store, time.Hour, time.Minute, 100)
	require.NoError(t, err)
	require.Equal(t, "outbox-pruner", p.Name())

	n, err := p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, n, "an enabled checkpoint holds the rows")

	store.mu.Lock()
	store.held = false
	store.mu.Unlock()
	n, err = p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(250), n, "batch by batch until a short batch")
	require.Equal(t, 4, store.prunes, "1 held, then 100, 100, 50")
}
