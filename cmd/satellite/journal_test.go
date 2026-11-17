package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

// The journal opens in the given directory and survives a restart: what
// was appended before Close is read back after the next openJournal.
func TestOpenJournalKeepsRecordsAcrossRestarts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	cm := newTestConfigManager(t, &config.Config{AppConfig: config.AppConfig{Journal: config.JournalConfig{MaxSegmentBytes: 4096, MaxTotalBytes: 1 << 20}}})

	j, err := openJournal(cm, dir)
	require.NoError(t, err)
	seq, err := j.Append(context.Background(), journal.Record{Type: edgejournal.RecordPullThrough, RecordedAt: time.Now(), Payload: json.RawMessage(`{"repository":"library/nginx"}`)})
	require.NoError(t, err)
	require.NoError(t, j.Close())
	require.DirExists(t, dir)

	j, err = openJournal(cm, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, j.Close()) })
	records, err := j.ReadFrom(0, 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, seq, records[0].Seq)
	require.JSONEq(t, `{"repository":"library/nginx"}`, string(records[0].Payload))
}

func TestOpenJournalFailsWhenTheDirectoryIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "journal")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err := openJournal(newTestConfigManager(t, &config.Config{}), file)
	require.ErrorContains(t, err, "create journal directory")
}
