package app_test

import (
	"os"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/app"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

// The OUTBOX_* defaults and DefaultPublisherConfig are written down twice;
// this keeps them equal.
func TestPublisherConfig_OutboxDefaultsMatchTheCode(t *testing.T) {
	previous := env.GC
	t.Cleanup(func() { env.GC = previous })
	for _, key := range []string{"OUTBOX_BATCH_SIZE", "OUTBOX_POLL_INTERVAL", "OUTBOX_CONCURRENCY", "OUTBOX_MAX_ATTEMPTS", "OUTBOX_BACKOFF_BASE", "OUTBOX_BACKOFF_MAX", "OUTBOX_LEASE"} {
		// t.Setenv restores the variable afterwards; the test needs it unset.
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
	require.NoError(t, env.LoadGC())

	require.Equal(t, async.DefaultPublisherConfig(), app.PublisherConfig(env.GC.Outbox))
}
