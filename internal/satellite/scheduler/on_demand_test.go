package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSchedulerWithoutInterval_RunsOnlyWhenTriggered(t *testing.T) {
	proc := &mockProcess{name: "on-demand"}
	sched, err := NewScheduler(proc, nopLogger())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)
	t.Cleanup(func() {
		cancel()
		require.NoError(t, sched.Stop(context.Background()))
	})

	time.Sleep(100 * time.Millisecond)
	require.Zero(t, proc.execCount.Load(), "the process ran on Start")

	sched.Trigger()
	require.Eventually(t, func() bool { return proc.execCount.Load() == 1 }, time.Second, 10*time.Millisecond)
}
