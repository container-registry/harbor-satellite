package heartbeat_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/stretchr/testify/require"
)

func newFlusher(t *testing.T, b *heartbeat.Buffer, s *store, c heartbeat.Config) *heartbeat.Flusher {
	t.Helper()
	f, err := heartbeat.NewFlusher(b, s, c)
	require.NoError(t, err)
	return f
}

func TestFlusher_FlushesOnInterval(t *testing.T) {
	c := cfg
	c.FlushInterval = 20 * time.Millisecond
	b := newBuffer(t, c)
	s := &store{}
	f := newFlusher(t, b, s, c)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()

	b.Record(beatAt(7, 0))

	require.Eventually(t, func() bool { return len(s.satellites()) == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestFlusher_FinalFlushAfterCancel(t *testing.T) {
	c := cfg
	c.FlushInterval = time.Hour
	b := newBuffer(t, c)
	s := &store{}
	f := newFlusher(t, b, s, c)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()

	b.Record(beatAt(7, 0))
	cancel()

	require.NoError(t, <-done)
	require.Equal(t, []fleet.SatelliteID{7}, s.satellites(), "the beat is flushed on a context without cancel")
}

func TestFlusher_PoisonBeatDoesNotBlockOthers(t *testing.T) {
	b := newBuffer(t, cfg)
	s := &store{poison: map[fleet.SatelliteID]bool{9: true}}
	f := newFlusher(t, b, s, cfg)

	for round := range cfg.MaxFlushFailures {
		b.Record(beatAt(7, time.Duration(round)*time.Second))
		b.Record(beatAt(9, time.Duration(round)*time.Second))
		_ = f.Flush(context.Background())
	}

	flushed := s.satellites()
	require.Len(t, flushed, cfg.MaxFlushFailures, "satellite 7 flushes every round")
	require.NotContains(t, flushed, fleet.SatelliteID(9))
	require.Empty(t, b.Drain().Latest, "the poison satellite is dropped after MaxFlushFailures")
}

// cancelStore fails the whole batch, then cancels the flush while it is
// retried satellite by satellite.
type cancelStore struct {
	cancel context.CancelFunc
	calls  int
}

func (s *cancelStore) Flush(ctx context.Context, _ heartbeat.Batch) error {
	s.calls++
	if s.calls == 2 {
		s.cancel()
		return ctx.Err()
	}
	return errBoom
}

func TestFlusher_CancelledRetryKeepsTheBeats(t *testing.T) {
	c := cfg
	c.MaxFlushFailures = 1
	b := newBuffer(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f, err := heartbeat.NewFlusher(b, &cancelStore{cancel: cancel}, c)
	require.NoError(t, err)

	b.Record(beatAt(7, 0))
	require.Error(t, f.Flush(ctx))

	require.Len(t, b.Drain().Latest, 1, "a cancelled flush is requeued, not counted as a failure")
}

func TestFlusher_DropResetsTheFailureCount(t *testing.T) {
	b := newBuffer(t, cfg)
	s := &store{poison: map[fleet.SatelliteID]bool{9: true}}
	f := newFlusher(t, b, s, cfg)

	for round := range cfg.MaxFlushFailures {
		b.Record(beatAt(9, time.Duration(round)*time.Second))
		_ = f.Flush(context.Background())
	}
	require.Empty(t, b.Drain().Latest, "dropped after MaxFlushFailures")

	b.Record(beatAt(9, time.Minute))
	_ = f.Flush(context.Background())
	require.Len(t, b.Drain().Latest, 1, "a later beat gets its own MaxFlushFailures retries")
}
