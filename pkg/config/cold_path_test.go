package config

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppConfig_ColdPathKeys(t *testing.T) {
	data, err := json.Marshal(AppConfig{LogLevel: "info"})
	require.NoError(t, err)
	var unset map[string]any
	require.NoError(t, json.Unmarshal(data, &unset))
	for _, key := range []string{"replication_concurrency", "replication_bandwidth_bytes_per_sec", "journal"} {
		require.NotContains(t, unset, key, "an unset key stays out of config.json")
	}

	var cfg AppConfig
	require.NoError(t, json.Unmarshal([]byte(`{
		"replication_concurrency": 2,
		"replication_bandwidth_bytes_per_sec": 1048576,
		"journal": {"max_segment_bytes": 1024, "max_total_bytes": 8192, "upload_batch": 50}
	}`), &cfg))
	require.Equal(t, 2, cfg.ReplicationConcurrency)
	require.EqualValues(t, 1<<20, cfg.ReplicationBandwidthBytesPerSec)
	require.Equal(t, JournalConfig{MaxSegmentBytes: 1024, MaxTotalBytes: 8192, UploadBatch: 50}, cfg.Journal)
}

func TestResolvePathConfig_JournalDirIsNotCreated(t *testing.T) {
	paths, err := ResolvePathConfig(filepath.Join(t.TempDir(), "satellite"))
	require.NoError(t, err)

	require.Equal(t, filepath.Join(paths.ConfigDir, "journal"), paths.JournalDir)
	require.NoDirExists(t, paths.JournalDir)
}
