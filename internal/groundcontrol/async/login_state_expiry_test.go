package async_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

type expiredLoginStates struct {
	mu    sync.Mutex
	calls []time.Time
}

func (e *expiredLoginStates) DeleteExpiredLoginStates(_ context.Context, now time.Time) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, now)
	return 1, nil
}

func (e *expiredLoginStates) seen() []time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]time.Time(nil), e.calls...)
}

func TestLoginStateExpirer_DeletesExpired(t *testing.T) {
	pending.Plan(t, "8.1", "there is no OIDC login; nothing stores login states")
	store := &expiredLoginStates{}
	e, err := async.NewLoginStateExpirer(store, fleettest.Clock{T: staleNow}, 10*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, "login-state-expirer", e.Name())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()

	require.Eventually(t, func() bool { return len(store.seen()) >= 2 }, time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	for _, now := range store.seen() {
		require.Equal(t, staleNow, now, "deletes by the clock's now")
	}
}
