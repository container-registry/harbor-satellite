package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthHandlerAlwaysOK(t *testing.T) {
	hr := NewHealthRegistrar("", "", false)

	rec := httptest.NewRecorder()
	hr.healthHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyHandler(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer registry.Close()

	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gc.Close()

	unreachable := "http://127.0.0.1:0"

	tests := []struct {
		name        string
		registryURL string
		gcURL       string
		headless    bool
		synced      bool
		wantCode    int
		wantStatus  string
		wantChecks  readyChecks
	}{
		{
			name:        "all checks pass",
			registryURL: registry.URL,
			gcURL:       gc.URL,
			synced:      true,
			wantCode:    http.StatusOK,
			wantStatus:  "ready",
			wantChecks:  readyChecks{Registry: "ok", GroundControl: "ok", StateSync: "ok"},
		},
		{
			name:        "state sync pending",
			registryURL: registry.URL,
			gcURL:       gc.URL,
			synced:      false,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
			wantChecks:  readyChecks{Registry: "ok", GroundControl: "ok", StateSync: "pending"},
		},
		{
			name:        "ground control unavailable",
			registryURL: registry.URL,
			gcURL:       unreachable,
			synced:      true,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
			wantChecks:  readyChecks{Registry: "ok", GroundControl: "unavailable", StateSync: "ok"},
		},
		{
			name:        "registry unavailable",
			registryURL: unreachable,
			gcURL:       gc.URL,
			synced:      true,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
			wantChecks:  readyChecks{Registry: "unavailable", GroundControl: "ok", StateSync: "ok"},
		},
		{
			name:        "headless skips ground control",
			registryURL: registry.URL,
			gcURL:       unreachable,
			headless:    true,
			synced:      true,
			wantCode:    http.StatusOK,
			wantStatus:  "ready",
			wantChecks:  readyChecks{Registry: "ok", GroundControl: "skipped", StateSync: "ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hr := NewHealthRegistrar(tt.registryURL, tt.gcURL, tt.headless)
			if tt.synced {
				hr.MarkStateSynced()
			}

			rec := httptest.NewRecorder()
			hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

			require.Equal(t, tt.wantCode, rec.Code)

			var resp readyResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.Equal(t, tt.wantStatus, resp.Status)
			require.Equal(t, tt.wantChecks, resp.Checks)
		})
	}
}

func TestReadyHandlerHeadlessDoesNotContactGroundControl(t *testing.T) {
	gcHit := false
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gcHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gc.Close()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer registry.Close()

	hr := NewHealthRegistrar(registry.URL, gc.URL, true)
	hr.MarkStateSynced()

	rec := httptest.NewRecorder()
	hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	require.False(t, gcHit, "ground control should not be contacted in headless mode")
}
