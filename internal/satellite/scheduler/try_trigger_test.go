package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestScheduler_TryTriggerDoesNotBlock(t *testing.T) {
	pending.Plan(t, "6.7", "Trigger is an unbuffered send that blocks until the scheduler loop receives, forever once it has exited")
	sched, err := NewScheduler(&mockProcess{name: "refresh_credentials"}, nopLogger())
	require.NoError(t, err)

	start := time.Now()
	first := sched.TryTrigger()
	second := sched.TryTrigger()
	require.Less(t, time.Since(start), 10*time.Millisecond)
	require.True(t, first, "the trigger waits for the loop")
	require.False(t, second, "a second trigger joins the waiting one")

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)
	cancel()
	require.NoError(t, sched.Stop(context.Background()))
	start = time.Now()
	require.False(t, sched.TryTrigger(), "a stopped scheduler takes no trigger")
	require.Less(t, time.Since(start), 10*time.Millisecond)
}
