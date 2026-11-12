package env

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var webhookKeys = map[string]string{
	"WEBHOOKS_ENABLED":      "true",
	"WEBHOOK_BATCH_SIZE":    "10",
	"WEBHOOK_POLL_INTERVAL": "2s",
	"WEBHOOK_BACKOFF_BASE":  "3s",
	"WEBHOOK_BACKOFF_MAX":   "1m",
	"WEBHOOK_TIMEOUT":       "5s",
	"WEBHOOK_DISABLE_AFTER": "12h",
	"WEBHOOK_SOURCE":        "urn:test",
	"OUTBOX_RETENTION":      "24h",
}

func TestLoadGCParsesWebhookKeys(t *testing.T) {
	previous := GC
	t.Cleanup(func() { GC = previous })

	t.Run("defaults", func(t *testing.T) {
		for key := range webhookKeys {
			t.Setenv(key, "")
			require.NoError(t, os.Unsetenv(key))
		}
		require.NoError(t, LoadGC())

		require.Equal(t, Webhook{
			BatchSize: 100, PollInterval: time.Second, BackoffBase: time.Second, BackoffMax: 5 * time.Minute,
			Timeout: 10 * time.Second, DisableAfter: 24 * time.Hour, Source: "urn:harbor-satellite:ground-control",
		}, GC.Webhook)
		require.Equal(t, 168*time.Hour, GC.Outbox.Retention)
	})

	t.Run("set", func(t *testing.T) {
		for key, value := range webhookKeys {
			t.Setenv(key, value)
		}
		require.NoError(t, LoadGC())

		require.Equal(t, Webhook{
			Enabled: true, BatchSize: 10, PollInterval: 2 * time.Second, BackoffBase: 3 * time.Second, BackoffMax: time.Minute,
			Timeout: 5 * time.Second, DisableAfter: 12 * time.Hour, Source: "urn:test",
		}, GC.Webhook)
		require.Equal(t, 24*time.Hour, GC.Outbox.Retention)
	})
}
