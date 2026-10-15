package satellite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestSatellite_StopStopsEventSchedulers(t *testing.T) {
	pending.Plan(t, "6.7", "Run starts the event schedulers on context.Background() and Stop never stops them")
	var refreshes atomic.Int32
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/refresh") {
			refreshes.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(gc.Close)
	dir := t.TempDir()
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "", gc.URL, false, &config.Config{
		StateConfig: config.StateConfig{
			RegistryCredentials: config.RegistryCredentials{URL: "http://harbor.invalid", Username: "robot$edge-01", Password: "secret"},
		},
		AppConfig: config.AppConfig{
			GroundControlURL:         config.URL(gc.URL),
			UseUnsecure:              true,
			StateReplicationInterval: "@every 1h",
			HeartbeatInterval:        "@every 1h",
		},
	})
	require.NoError(t, err)
	log := zerolog.Nop()
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), logger.LoggerKey, &log))
	es := events.NewEventScheduler(&log)
	s := NewSatellite(cm, nil, "", t.TempDir(), es)
	require.NoError(t, s.Run(ctx))
	// Let the runs every scheduler makes on start finish.
	time.Sleep(200 * time.Millisecond)

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	s.Stop(stopCtx)
	require.NoError(t, stopCtx.Err(), "Stop returned in time")
	before := refreshes.Load()

	es.SendEvent("refresh_credentials")
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, before, refreshes.Load(), "refresh_credentials ran after Stop")
}
