package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// mockProcess implements Process for testing.
type mockProcess struct {
	name      string
	running   atomic.Bool
	complete  atomic.Bool
	execCount atomic.Int32
	execDelay time.Duration
	execErr   error
	execFn    func(ctx context.Context) error
}

func (m *mockProcess) Name() string       { return m.name }
func (m *mockProcess) IsRunning() bool     { return m.running.Load() }
func (m *mockProcess) IsComplete() bool    { return m.complete.Load() }

func (m *mockProcess) Execute(ctx context.Context) error {
	m.running.Store(true)
	defer m.running.Store(false)
	m.execCount.Add(1)

	if m.execFn != nil {
		return m.execFn(ctx)
	}

	if m.execDelay > 0 {
		select {
		case <-time.After(m.execDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.execErr
}

func nopLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

func TestStop_WaitsForInflightProcess(t *testing.T) {
	execStarted := make(chan struct{})
	execDone := make(chan struct{})

	proc := &mockProcess{
		name: "slow-task",
		execFn: func(ctx context.Context) error {
			close(execStarted)
			<-execDone
			return nil
		},
	}

	sched, err := NewSchedulerWithInterval("@every 1h", proc, nopLogger())
	require.NoError(t, err)
	sched.jitterFn = func(int64) int64 { return 0 } // zero jitter: fire immediately

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)

	// Wait for the initial execution to start
	<-execStarted

	// Cancel context to stop the scheduler loop
	cancel()

	// Stop should block until the in-flight process completes
	stopDone := make(chan error, 1)
	go func() {
		stopDone <- sched.Stop(context.Background())
	}()

	// Verify Stop is still blocking
	select {
	case <-stopDone:
		t.Fatal("Stop returned before in-flight process completed")
	case <-time.After(50 * time.Millisecond):
	}

	// Let the process finish
	close(execDone)

	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after process completed")
	}
}

func TestStop_RespectsContextTimeout(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)

	proc := &mockProcess{
		name: "stuck-task",
		execFn: func(_ context.Context) error {
			// Simulate a process that ignores context cancellation
			<-blocked
			return nil
		},
	}

	sched, err := NewSchedulerWithInterval("@every 1h", proc, nopLogger())
	require.NoError(t, err)
	sched.jitterFn = func(int64) int64 { return 0 } // zero jitter: fire immediately

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)

	// Give the scheduler time to launch its first execution
	time.Sleep(50 * time.Millisecond)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopCancel()

	err = sched.Stop(stopCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestStart_ImmediateStopNoRace(t *testing.T) {
	proc := &mockProcess{
		name:      "quick-task",
		execDelay: 10 * time.Millisecond,
	}

	sched, err := NewSchedulerWithInterval("@every 1h", proc, nopLogger())
	require.NoError(t, err)

	// Start and immediately stop -- this must not race.
	// Before the fix, wg.Add(1) was inside the goroutine, so Stop
	// could see counter=0 and return before Run even started.
	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	err = sched.Stop(stopCtx)
	require.NoError(t, err)
}

func TestContextCancellation_StopsScheduler(t *testing.T) {
	proc := &mockProcess{name: "counting-task"}

	sched, err := NewSchedulerWithInterval("@every 50ms", proc, nopLogger())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)

	// Let a few executions happen
	time.Sleep(200 * time.Millisecond)
	cancel()

	err = sched.Stop(context.Background())
	require.NoError(t, err)

	countAtStop := proc.execCount.Load()
	require.Greater(t, countAtStop, int32(1), "should have executed more than once")

	// After stop, no more executions should occur
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, countAtStop, proc.execCount.Load(), "no executions after stop")
}

func TestMultipleSchedulers_GracefulShutdown(t *testing.T) {
	var wg sync.WaitGroup
	schedulers := make([]*Scheduler, 3)

	for i := range schedulers {
		proc := &mockProcess{
			name:      "task",
			execDelay: 20 * time.Millisecond,
		}
		s, err := NewSchedulerWithInterval("@every 50ms", proc, nopLogger())
		require.NoError(t, err)
		schedulers[i] = s
	}

	ctx, cancel := context.WithCancel(context.Background())

	for _, s := range schedulers {
		s.Start(ctx)
	}

	// Let them run
	time.Sleep(150 * time.Millisecond)
	cancel()

	// Stop all in parallel (mirrors Satellite.Stop behavior)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	for _, s := range schedulers {
		wg.Add(1)
		go func(sched *Scheduler) {
			defer wg.Done()
			err := sched.Stop(stopCtx)
			require.NoError(t, err)
		}(s)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for all schedulers to stop")
	}
}

// TestScheduler_JitterBounds verifies that the startup jitter is drawn from
// [0, interval) — never negative, never equal to or beyond the interval.
func TestScheduler_JitterBounds(t *testing.T) {
	var capturedMax int64
	proc := &mockProcess{name: "probe"}

	sched, err := NewSchedulerWithInterval("@every 10s", proc, nopLogger())
	require.NoError(t, err)

	sched.jitterFn = func(n int64) int64 {
		capturedMax = n
		return n / 2 // deterministic midpoint
	}

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)
	// Wait long enough for the jitter (5s midpoint) to elapse, then cancel.
	// We don't actually wait 5s in the test — we just verify the jitterFn was
	// called with the right bound and cancel immediately.
	cancel()
	_ = sched.Stop(context.Background())

	require.Equal(t, int64(10*time.Second), capturedMax,
		"jitter upper bound must equal the scheduler interval")
}

// TestScheduler_ContextCancelDuringJitter verifies that cancelling the context
// while the scheduler is sleeping through its startup jitter causes a clean exit
// without ever firing the process.
func TestScheduler_ContextCancelDuringJitter(t *testing.T) {
	proc := &mockProcess{name: "never-runs"}

	sched, err := NewSchedulerWithInterval("@every 1h", proc, nopLogger())
	require.NoError(t, err)
	// Full-interval jitter: the scheduler would wait 1h before first execution.
	sched.jitterFn = func(n int64) int64 { return n }

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)

	// Cancel immediately — scheduler must exit during jitter, not hang.
	cancel()

	stopDone := make(chan struct{})
	go func() {
		_ = sched.Stop(context.Background())
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// exited cleanly during jitter window
	case <-time.After(500 * time.Millisecond):
		t.Fatal("scheduler did not exit after context cancel during jitter")
	}

	require.Zero(t, proc.execCount.Load(), "process must not fire when context is cancelled during jitter")
}

// TestScheduler_JitterSpreadsFirstFire simulates the fleet-restart scenario:
// N schedulers start at the same instant with different jitter offsets and their
// first execution times must be spread across [0, interval), not all at T=0.
func TestScheduler_JitterSpreadsFirstFire(t *testing.T) {
	const numSatellites = 10
	const interval = 100 * time.Millisecond

	firstFireTimes := make([]time.Duration, numSatellites)
	start := time.Now()

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range numSatellites {
		wg.Add(1)
		idx := i
		proc := &mockProcess{name: "satellite"}
		proc.execFn = func(_ context.Context) error {
			mu.Lock()
			firstFireTimes[idx] = time.Since(start)
			mu.Unlock()
			return nil
		}

		sched, err := NewSchedulerWithInterval("@every 100ms", proc, nopLogger())
		require.NoError(t, err)
		// Each satellite gets a fixed, distinct jitter offset.
		fixedJitter := time.Duration(idx) * (interval / numSatellites)
		sched.jitterFn = func(int64) int64 { return int64(fixedJitter) }

		ctx, cancel := context.WithTimeout(context.Background(), interval*3)
		sched.Start(ctx)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			_ = sched.Stop(context.Background())
			cancel()
		}()
	}

	wg.Wait()

	// The spread between the earliest and latest first-fire must be at least
	// half the interval. With 10 evenly-spaced offsets over 100ms (0,10,20,...90ms)
	// the real spread should be ~90ms. Requiring ≥50ms gives plenty of headroom
	// for scheduling jitter while still proving the fires are not clustered at T=0.
	var earliest, latest time.Duration
	for i, ft := range firstFireTimes {
		if i == 0 || ft < earliest {
			earliest = ft
		}
		if ft > latest {
			latest = ft
		}
	}
	spread := latest - earliest
	require.GreaterOrEqual(t, spread, interval/2,
		"first-fire times are not spread out: earliest=%v latest=%v spread=%v (want ≥%v)",
		earliest, latest, spread, interval/2)
}
