package env

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var fleetHotPathKeys = map[string]string{
	"SATELLITE_SESSION_TTL":        "48h",
	"SATELLITE_SESSION_CACHE_TTL":  "10s",
	"SATELLITE_SESSION_CACHE_SIZE": "500",
	"SATELLITE_ROBOT_AUTH":         "false",
	"HEARTBEAT_FLUSH_INTERVAL":     "2s",
	"HEARTBEAT_BUFFER_MAX":         "20",
	"HEARTBEAT_MAX_FLUSH_FAILURES": "5",
	"STALE_THRESHOLD":              "30m",
	"STALE_CHECK_INTERVAL":         "30s",
	"SESSION_EXPIRY_INTERVAL":      "2h",
	"STATUS_RETENTION_DAYS":        "14",
	"STATUS_PARTITIONS_AHEAD":      "5",
	"RATE_LIMIT_SPLIT":             "true",
	"RATE_LIMIT_ANONYMOUS":         "5/1m",
	"RATE_LIMIT_FLEET":             "6/1s",
}

func TestLoadGCParsesFleetHotPathKeys(t *testing.T) {
	previous := GC
	t.Cleanup(func() { GC = previous })

	t.Run("defaults", func(t *testing.T) {
		for key := range fleetHotPathKeys {
			// t.Setenv restores the variable afterwards; the test needs it unset.
			t.Setenv(key, "")
			require.NoError(t, os.Unsetenv(key))
		}
		require.NoError(t, LoadGC())

		require.Equal(t, FleetHotPath{
			SatelliteSessionTTL:       720 * time.Hour,
			SatelliteSessionCacheTTL:  30 * time.Second,
			SatelliteSessionCacheSize: 10000,
			SatelliteRobotAuth:        true,
			HeartbeatFlushInterval:    5 * time.Second,
			HeartbeatBufferMax:        10000,
			HeartbeatMaxFlushFailures: 3,
			StaleCheckInterval:        time.Minute,
			SessionExpiryInterval:     time.Hour,
			StatusRetentionDays:       7,
			StatusPartitionsAhead:     3,
			RateLimitSplit:            false,
			RateLimitAnonymous:        "10/1m",
			RateLimitFleet:            "3/1s",
		}, GC.Server.FleetHotPath)
		require.Equal(t, 15*time.Minute, GC.Server.StaleThreshold, "today's effective fallback of the stale queries")
	})

	t.Run("set", func(t *testing.T) {
		for key, value := range fleetHotPathKeys {
			t.Setenv(key, value)
		}
		require.NoError(t, LoadGC())

		require.Equal(t, FleetHotPath{
			SatelliteSessionTTL:       48 * time.Hour,
			SatelliteSessionCacheTTL:  10 * time.Second,
			SatelliteSessionCacheSize: 500,
			SatelliteRobotAuth:        false,
			HeartbeatFlushInterval:    2 * time.Second,
			HeartbeatBufferMax:        20,
			HeartbeatMaxFlushFailures: 5,
			StaleCheckInterval:        30 * time.Second,
			SessionExpiryInterval:     2 * time.Hour,
			StatusRetentionDays:       14,
			StatusPartitionsAhead:     5,
			RateLimitSplit:            true,
			RateLimitAnonymous:        "5/1m",
			RateLimitFleet:            "6/1s",
		}, GC.Server.FleetHotPath)
		require.Equal(t, 30*time.Minute, GC.Server.StaleThreshold)
	})
}
