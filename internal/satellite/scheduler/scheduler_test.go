package scheduler

import (
	"context"
	"runtime"
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

func (m *mockProcess) Name() string     { return m.name }
func (m *mockProcess) IsRunning() bool  { return m.running.Load() }
func (m *mockProcess) IsComplete() bool { return m.complete.Load() }

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

func TestNonPositiveIntervalIsAnErrorNotAPanic(t *testing.T) {
	for _, expr := range []string{"@every 0s", "@every -5s", "@every 0h00m00s"} {
		_, err := NewSchedulerWithInterval(expr, &mockProcess{name: "p"}, nopLogger())
		require.ErrorContains(t, err, "must be positive", expr)
	}

	s, err := NewSchedulerWithInterval("@every 1h", &mockProcess{name: "p"}, nopLogger())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Stop(context.Background())) })
	require.NotPanics(t, func() {
		require.ErrorContains(t, s.ResetIntervalFromExpr("@every 0s"), "must be positive")
	})
}

// A process marks itself running only once its goroutine runs Execute, so
// the scheduler must not rely on IsRunning alone: two triggers in a row
// would otherwise start two runs of one process at the same time.
func TestLaunchProcess_NeverRunsTheProcessTwiceAtOnce(t *testing.T) {
	// One P keeps the first run's goroutine from starting before the
	// second launch, the window a busy machine opens by chance.
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	release := make(chan struct{})
	var active, peak atomic.Int32
	proc := &mockProcess{name: "replication", execFn: func(context.Context) error {
		n := active.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		<-release
		active.Add(-1)
		return nil
	}}
	s, err := NewScheduler(proc, nopLogger())
	require.NoError(t, err)

	s.launchProcess(context.Background())
	s.launchProcess(context.Background())
	require.Eventually(t, func() bool { return proc.execCount.Load() >= 1 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	close(release)
	s.wg.Wait()

	require.EqualValues(t, 1, peak.Load(), "runs at once")
	require.EqualValues(t, 1, proc.execCount.Load())
}

func TestParseEveryExpr(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantDur time.Duration
		wantErr bool
	}{
		{
			name:    "valid 30s",
			expr:    "@every 30s",
			wantDur: 30 * time.Second,
		},
		{
			name:    "valid complex",
			expr:    "@every 00h01m30s",
			wantDur: 90 * time.Second,
		},
		{
			name:    "empty",
			expr:    "",
			wantErr: true,
		},
		{
			name:    "missing prefix",
			expr:    "30s",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := parseEveryExpr(tt.expr)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDur, d)
		})
	}
}
