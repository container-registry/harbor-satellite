package async_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingPublisher = "the publisher is a stub; handlers still call Harbor in the request"

func TestDefaultPublisherConfig(t *testing.T) {
	cfg := async.DefaultPublisherConfig()

	require.Positive(t, cfg.BatchSize)
	require.Positive(t, cfg.Concurrency)
	require.Positive(t, cfg.MaxAttempts)
	require.Less(t, cfg.BackoffBase, cfg.BackoffMax)
	require.Greater(t, cfg.Lease, cfg.PollInterval)
}

func TestBackoff_ExponentialCapped(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	for attempt, want := range map[int]time.Duration{
		0:   time.Second,
		1:   time.Second,
		2:   2 * time.Second,
		3:   4 * time.Second,
		9:   256 * time.Second,
		10:  5 * time.Minute,
		100: 5 * time.Minute,
	} {
		require.Equal(t, want, async.Backoff(attempt, time.Second, 5*time.Minute), "attempt %d", attempt)
	}
}

// newPublisher runs against a reachable registry and a free locker.
func newPublisher(t *testing.T, store async.OutboxStore, h async.Handler, cfg async.PublisherConfig) *async.Publisher {
	t.Helper()
	p, err := async.NewPublisher(async.PublisherDeps{Store: store, Handler: h, Locker: newFakeLocker(), Registry: &fakeReachability{reachable: true}}, cfg)
	require.NoError(t, err)
	return p
}

func TestPublisher_CompletesHandledEvents(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	store := newFakeOutbox(fleet.EventSatelliteCreated, fleet.EventGroupChanged, fleet.EventConfigChanged)
	var handled []int64
	var mu sync.Mutex
	p := newPublisher(t, store, async.HandlerFunc(func(_ context.Context, e async.OutboxEvent) error {
		mu.Lock()
		defer mu.Unlock()
		handled = append(handled, e.ID)
		return nil
	}), async.DefaultPublisherConfig())

	n, err := p.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.ElementsMatch(t, []int64{1, 2, 3}, handled)
	require.Equal(t, []int64{1, 2, 3}, store.completed())
	require.Empty(t, store.failures)
}

func TestPublisher_BacksOffFailures(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	store := newFakeOutbox(fleet.EventSatelliteCreated, fleet.EventSatelliteCreated)
	cfg := async.DefaultPublisherConfig()
	cfg.BackoffBase, cfg.BackoffMax = 10*time.Second, time.Hour
	p := newPublisher(t, store, async.HandlerFunc(func(_ context.Context, e async.OutboxEvent) error {
		if e.ID == 2 {
			return errors.New("registry down")
		}
		return nil
	}), cfg)

	_, err := p.RunOnce(context.Background())

	require.NoError(t, err, "a failing handler does not fail the batch")
	require.Equal(t, []int64{1}, store.completed())
	require.Len(t, store.failures, 1)
	f := store.failures[0]
	require.EqualValues(t, 2, f.ID)
	require.Contains(t, f.Cause, "registry down")
	require.Equal(t, async.Backoff(1, cfg.BackoffBase, cfg.BackoffMax), f.RetryIn, "the first attempt waits the base backoff")
	require.Equal(t, cfg.BackoffBase, f.RetryIn)
}

func TestPublisher_StopsAtMaxAttempts(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	store := newFakeOutbox(fleet.EventSatelliteCreated)
	cfg := async.DefaultPublisherConfig()
	cfg.MaxAttempts = 3
	var calls atomic.Int32
	p := newPublisher(t, store, async.HandlerFunc(func(context.Context, async.OutboxEvent) error {
		calls.Add(1)
		return errors.New("always fails")
	}), cfg)

	for range 5 {
		_, err := p.RunOnce(context.Background())
		require.NoError(t, err)
	}

	require.EqualValues(t, 3, calls.Load())
	require.Len(t, store.failures, 3)
	for _, maxAttempts := range store.claims {
		require.Equal(t, 3, maxAttempts)
	}
}

func TestPublisher_RespectsConcurrencyLimit(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	kinds := make([]fleet.EventKind, 12)
	for i := range kinds {
		kinds[i] = fleet.EventSatelliteCreated
	}
	store := newFakeOutbox(kinds...)
	cfg := async.DefaultPublisherConfig()
	cfg.Concurrency = 3
	var running, peak atomic.Int32
	p := newPublisher(t, store, async.HandlerFunc(func(context.Context, async.OutboxEvent) error {
		n := running.Add(1)
		for {
			if old := peak.Load(); n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return nil
	}), cfg)

	n, err := p.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 12, n)
	require.EqualValues(t, 3, peak.Load())
}

func TestPublisher_RunStopsWhenTheContextEnds(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	store := newFakeOutbox(fleet.EventSatelliteCreated)
	cfg := async.DefaultPublisherConfig()
	cfg.PollInterval = 10 * time.Millisecond
	p := newPublisher(t, store, async.HandlerFunc(func(context.Context, async.OutboxEvent) error { return nil }), cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- p.Run(ctx) }()
	require.Eventually(t, func() bool { return len(store.completed()) == 1 }, 5*time.Second, 10*time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context ended")
	}
}

func TestPublisher_CompletesEventsWithoutHandler(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	store := newFakeOutbox("SomethingNew")
	p := newPublisher(t, store, async.HandlerFunc(func(context.Context, async.OutboxEvent) error {
		return fmt.Errorf("route: %w", async.ErrNoHandler)
	}), async.DefaultPublisherConfig())

	_, err := p.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, []int64{1}, store.completed(), "logged and completed, not retried")
	require.Empty(t, store.failures)
}

// Another worker reconciles the aggregate right now: the event waits
// without paying an attempt (A86).
func TestPublisher_ReleasesEventsOfLockedAggregates(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	store := newFakeOutbox(fleet.EventSatelliteCreated, fleet.EventSatelliteCreated)
	locker := newFakeLocker()
	locker.held[fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 2}] = true
	var handled []int64
	p, err := async.NewPublisher(async.PublisherDeps{
		Store: store,
		Handler: async.HandlerFunc(func(_ context.Context, e async.OutboxEvent) error {
			handled = append(handled, e.ID)
			return nil
		}),
		Locker:   locker,
		Registry: &fakeReachability{reachable: true},
	}, async.DefaultPublisherConfig())
	require.NoError(t, err)

	_, err = p.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, []int64{1}, handled)
	require.Equal(t, []int64{1}, store.completed())
	require.Len(t, store.releases, 1)
	require.EqualValues(t, 2, store.releases[0].ID)
	require.Empty(t, store.failures)
	require.Equal(t, 1, locker.taken[fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 1}])
}

// A Harbor outage longer than the whole backoff budget dead-letters
// nothing: unreachable failures give their attempt back, claiming pauses
// while the probe is stale, and recovery re-arms what ran out (A82).
func TestPublisher_ConvergesAfterALongOutage(t *testing.T) {
	pending.Plan(t, "2.3", pendingPublisher)
	store := newFakeOutbox(fleet.EventSatelliteCreated, fleet.EventGroupChanged)
	store.events[1].Attempts = 3
	reach := &fakeReachability{reachable: true}
	var harborUp atomic.Bool
	cfg := async.DefaultPublisherConfig()
	cfg.MaxAttempts = 3
	p, err := async.NewPublisher(async.PublisherDeps{
		Store: store,
		Handler: async.HandlerFunc(func(context.Context, async.OutboxEvent) error {
			if !harborUp.Load() {
				return fmt.Errorf("push: %w", fleet.ErrRegistryUnavailable)
			}
			return nil
		}),
		Locker:   newFakeLocker(),
		Registry: reach,
	}, cfg)
	require.NoError(t, err)

	for range 10 {
		_, err := p.RunOnce(context.Background())
		require.NoError(t, err)
		store.expire()
	}
	require.Empty(t, store.failures, "an unreachable registry is no attempt")
	require.NotEmpty(t, store.releases)

	reach.set(false)
	claims := len(store.claims)
	_, err = p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Len(t, store.claims, claims, "no claims while the probe is stale")

	harborUp.Store(true)
	reach.set(true)
	for range 3 {
		_, err := p.RunOnce(context.Background())
		require.NoError(t, err)
		store.expire()
	}
	require.Positive(t, store.rearms, "recovery re-arms events that ran out of attempts")
	require.Equal(t, []int64{1, 2}, store.completed())
}
