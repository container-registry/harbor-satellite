package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStateConfig_GCSessionRoundTrip(t *testing.T) {
	cfg := &Config{
		StateConfig: StateConfig{StateURL: "http://harbor/satellite/satellite-state/edge-01/state:latest"},
		AppConfig:   AppConfig{LogLevel: "info"},
	}
	path := filepath.Join(t.TempDir(), "config.json")
	cm, err := NewConfigManager(path, "", "", "", false, cfg)
	require.NoError(t, err)
	require.NoError(t, cm.WriteConfig())
	before, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)

	cm.With(SetGCSession(GCSession{}))
	require.NoError(t, cm.WriteConfig())
	unchanged, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	require.Equal(t, string(before), string(unchanged), "an empty session leaves config.json unchanged")
	require.False(t, bytes.Contains(unchanged, []byte("gc_session")))

	session := GCSession{Token: "hss_token", ExpiresAt: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	cm.With(SetGCSession(session))
	require.Equal(t, session, cm.GetGCSession())
	require.NoError(t, cm.WriteConfig())
	reloaded, _, err := InitConfigManager("", "https://gc.example", path, "", false, false)
	require.NoError(t, err)
	require.Equal(t, session.Token, reloaded.GetGCSession().Token)
	require.True(t, session.ExpiresAt.Equal(reloaded.GetGCSession().ExpiresAt))
	require.Equal(t, cfg.StateConfig.StateURL, reloaded.GetStateURL(), "the rest of state_config is kept")
}
