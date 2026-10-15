package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// newRefreshConfig writes a satellite config for a token satellite talking
// to gcURL and returns its manager and file.
func newRefreshConfig(t *testing.T, gcURL string) (*config.ConfigManager, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cm, err := config.NewConfigManager(path, filepath.Join(dir, "prev.json"), "", gcURL, false, &config.Config{
		StateConfig: config.StateConfig{
			RegistryCredentials: config.RegistryCredentials{URL: "http://harbor.test", Username: "robot$edge-01", Password: "0ld"},
		},
		AppConfig: config.AppConfig{GroundControlURL: config.URL(gcURL), UseUnsecure: true},
	})
	require.NoError(t, err)
	return cm, path
}

func TestRefreshCredentialsEvent_NotRunOnSatelliteStart(t *testing.T) {
	pending.Plan(t, "D2", "registering refresh_credentials runs it at once, so every satellite start rotates the robot secret once the refresh route works")
	var requests atomic.Int32
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"secret":"n3w"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(gc.Close)
	cm, _ := newRefreshConfig(t, gc.URL)
	log := zerolog.Nop()
	sched, err := NewRefreshCredentialsEvent(cm, &log)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		require.NoError(t, sched.Stop(context.Background()))
	})

	NewEventScheduler(&log).Register(ctx, sched)

	time.Sleep(200 * time.Millisecond)
	require.Zero(t, requests.Load(), "refresh_credentials called Ground Control without a refresh_credentials event")
}

func TestRefreshCredentialProcess_PersistsSecret(t *testing.T) {
	pending.Plan(t, "D2", "the satellite posts the credential refresh to /sat/refresh, which Ground Control does not serve")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /satellites/refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"secret":"n3w"}`))
		require.NoError(t, err)
	})
	gc := httptest.NewServer(mux)
	t.Cleanup(gc.Close)
	cm, path := newRefreshConfig(t, gc.URL)
	log := zerolog.Nop()
	p := newTestRefreshProcess()
	p.cm = cm
	p.log = &log

	require.NoError(t, p.Execute(context.Background()))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var saved config.Config
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, "n3w", saved.StateConfig.RegistryCredentials.Password)
}
