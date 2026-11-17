package async

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvery_SkipsTheRunOnceCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runs := 0
	every(ctx, time.Millisecond, func(context.Context) { runs++ })
	require.Zero(t, runs, "a canceled worker started a pass")
}

// The ticker fires while fn runs; select may then take it over ctx.Done.
func TestEvery_StopsAfterThePassThatSawCancellation(t *testing.T) {
	for range 50 {
		ctx, cancel := context.WithCancel(context.Background())
		runs := 0
		every(ctx, time.Millisecond, func(context.Context) {
			runs++
			cancel()
			time.Sleep(3 * time.Millisecond)
		})
		require.Equal(t, 1, runs, "a pass started after cancellation")
	}
}
