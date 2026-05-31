package logger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readAuditLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		out = append(out, entry)
	}
	return out
}

func TestAuditLogger_WritesStructuredEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{
		Enabled:    true,
		FilePath:   path,
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
	})
	require.NoError(t, err)
	require.True(t, a.Enabled())

	a.Log(EventUserLoginFailure, "alice", "10.0.0.5", map[string]any{
		"reason": "bad_password",
	})

	entries := readAuditLines(t, path)
	require.Len(t, entries, 1)
	e := entries[0]

	require.Equal(t, string(EventUserLoginFailure), e["event_type"])
	require.Equal(t, "alice", e["actor"])
	require.Equal(t, "10.0.0.5", e["source_ip"])
	require.NotEmpty(t, e["event_id"])
	require.NotEmpty(t, e["timestamp"])
	_, err = time.Parse(time.RFC3339Nano, e["timestamp"].(string))
	require.NoError(t, err)

	details, ok := e["details"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "bad_password", details["reason"])
}

func TestAuditLogger_UniqueEventIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: path})
	require.NoError(t, err)

	for range 5 {
		a.Log(EventSatelliteRegistered, "sat-01", "10.0.0.1", nil)
	}

	entries := readAuditLines(t, path)
	require.Len(t, entries, 5)

	seen := make(map[string]struct{})
	for _, e := range entries {
		id := e["event_id"].(string)
		_, dup := seen[id]
		require.False(t, dup, "duplicate event_id %s", id)
		seen[id] = struct{}{}
	}
}

func TestAuditLogger_DisabledIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: false, FilePath: path})
	require.NoError(t, err)
	require.False(t, a.Enabled())

	a.Log(EventUserLoginFailure, "alice", "10.0.0.5", nil)

	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "no file should be created when disabled")
}

func TestAuditLogger_EmptyPathIsNoOp(t *testing.T) {
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: ""})
	require.NoError(t, err)
	require.False(t, a.Enabled())
	a.Log(EventUserLoginSuccess, "alice", "10.0.0.5", nil)
}

func TestAuditLogger_UnwritableDestinationErrors(t *testing.T) {
	// Use a regular file in place of a parent directory so MkdirAll fails with
	// ENOTDIR. Construction must surface an error rather than returning a logger
	// that silently drops every event.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	a, err := NewAuditLogger(AuditConfig{
		Enabled:  true,
		FilePath: filepath.Join(blocker, "audit.log"),
	})
	require.Error(t, err)
	require.Nil(t, a)
}

func TestAuditLogger_ReconfigureSwapsDestination(t *testing.T) {
	// Start disabled, then enable via Reconfigure (the hot-reload path).
	a, err := NewAuditLogger(AuditConfig{Enabled: false})
	require.NoError(t, err)
	require.False(t, a.Enabled())

	path := filepath.Join(t.TempDir(), "audit.log")
	require.NoError(t, a.Reconfigure(AuditConfig{Enabled: true, FilePath: path}))
	require.True(t, a.Enabled())

	a.Log(EventUserLoginSuccess, "alice", "10.0.0.5", nil)
	require.Len(t, readAuditLines(t, path), 1)

	// Disable again; further events must not be written.
	require.NoError(t, a.Reconfigure(AuditConfig{Enabled: false}))
	require.False(t, a.Enabled())
	a.Log(EventUserLoginSuccess, "bob", "10.0.0.6", nil)
	require.Len(t, readAuditLines(t, path), 1)
}

func TestAuditLogger_NilReceiverSafe(t *testing.T) {
	var a *AuditLogger
	require.NotPanics(t, func() {
		a.Log(EventUserLoginSuccess, "x", "y", nil)
	})
	require.False(t, a.Enabled())
}

func TestAuditFromContext_DefaultsToNoOp(t *testing.T) {
	ctx := context.Background()
	a := AuditFromContext(ctx)
	require.NotNil(t, a)
	require.False(t, a.Enabled())
}

func TestAuditFromContext_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: path})
	require.NoError(t, err)

	ctx := WithAuditLogger(context.Background(), a)
	got := AuditFromContext(ctx)
	require.Same(t, a, got)
}
