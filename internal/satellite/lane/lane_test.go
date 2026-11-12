package lane_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/lane"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func newLane(t *testing.T, opts lane.Options) *lane.Lane {
	t.Helper()
	l, err := lane.New("test", opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = l.Close(ctx)
	})
	return l
}

func TestLane_BoundsConcurrency(t *testing.T) {
	pending.Plan(t, "5.3", "prefetch has no bounded lane")
	l := newLane(t, lane.Options{Concurrency: 2, QueueSize: 10})
	var running, peak atomic.Int32
	var done sync.WaitGroup
	release := make(chan struct{})
	for range 6 {
		done.Add(1)
		require.NoError(t, l.Submit(context.Background(), func(context.Context) error {
			defer done.Done()
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			<-release
			running.Add(-1)
			return nil
		}))
	}

	require.Eventually(t, func() bool { return running.Load() == 2 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	require.EqualValues(t, 2, running.Load())
	close(release)
	done.Wait()
	require.EqualValues(t, 2, peak.Load())
}

func TestLane_SubmitFullReturnsErrLaneFull(t *testing.T) {
	pending.Plan(t, "5.3", "prefetch has no bounded lane")
	l := newLane(t, lane.Options{Concurrency: 1, QueueSize: 1})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan struct{})
	block := func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return nil
	}

	require.NoError(t, l.Submit(context.Background(), block))
	<-started
	require.NoError(t, l.Submit(context.Background(), block))
	require.ErrorIs(t, l.Submit(context.Background(), block), lane.ErrLaneFull)
}

func TestLane_CloseWaitsForJobs(t *testing.T) {
	pending.Plan(t, "5.3", "prefetch has no bounded lane")
	l, err := lane.New("test", lane.Options{Concurrency: 1, QueueSize: 2})
	require.NoError(t, err)
	var finished atomic.Int32
	for range 3 {
		require.NoError(t, l.Submit(context.Background(), func(context.Context) error {
			time.Sleep(20 * time.Millisecond)
			finished.Add(1)
			return nil
		}))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, l.Close(ctx))
	require.EqualValues(t, 3, finished.Load())
	require.ErrorIs(t, l.Submit(context.Background(), func(context.Context) error { return nil }), lane.ErrLaneClosed)
}
