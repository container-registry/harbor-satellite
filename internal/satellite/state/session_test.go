package state

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

const pendingSession = "Ground Control issues no sessions; the satellite authenticates every beat with its robot"

var sessionExpiry = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func sessionJSON(token string) map[string]any {
	return map[string]any{"token": token, "expires_at": sessionExpiry.Format(time.RFC3339)}
}

func TestRegistration_StoresGCSession(t *testing.T) {
	pending.Plan(t, "4.1", pendingSession)
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"auth":    map[string]string{"url": "http://registry", "username": "robot$edge-01", "password": "secret"},
			"state":   "http://registry/satellite/satellite-state/edge-01/state:latest",
			"session": sessionJSON("hss_first"),
		}))
	}))
	t.Cleanup(gc.Close)
	dir := t.TempDir()
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "token", gc.URL, false,
		&config.Config{AppConfig: config.AppConfig{GroundControlURL: config.URL(gc.URL), UseUnsecure: true}})
	require.NoError(t, err)

	require.NoError(t, NewZtrProcess(cm).Execute(testContext()))

	session := cm.GetGCSession()
	require.Equal(t, "hss_first", session.Token)
	require.True(t, sessionExpiry.Equal(session.ExpiresAt))
	require.Equal(t, "robot$edge-01", cm.GetStateConfig().RegistryCredentials.Username, "the robot still pulls from Harbor")
}

func TestStatusReport_UsesRenewedSession(t *testing.T) {
	pending.Plan(t, "4.1", pendingSession)
	var (
		mu    sync.Mutex
		auths []string
	)
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		first := len(auths) == 1
		mu.Unlock()
		resp := map[string]any{"events": []string{}}
		if first {
			resp["session"] = sessionJSON("hss_renewed")
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(gc.Close)
	cm := newReportingTestCM(t, gc.URL)
	cm.With(config.SetGCSession(config.GCSession{Token: "hss_old", ExpiresAt: sessionExpiry}))
	p := &StatusReportingProcess{name: "test", mu: &sync.Mutex{}, cm: cm}

	require.NoError(t, p.Execute(testContext()))
	require.NoError(t, p.Execute(testContext()))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"Bearer hss_old", "Bearer hss_renewed"}, auths)
	require.Equal(t, "hss_renewed", cm.GetGCSession().Token, "the renewal is stored")
}
