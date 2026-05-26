package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeReady(t *testing.T, body []byte) (string, map[string]string) {
	t.Helper()
	var resp struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("invalid JSON response %q: %v", body, err)
	}
	return resp.Status, resp.Checks
}

func TestHealthHandlerAlwaysOK(t *testing.T) {
	hr := NewHealthRegistrar("", "", false)

	rec := httptest.NewRecorder()
	hr.healthHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
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
		wantChecks  map[string]string
	}{
		{
			name:        "all ok",
			registryURL: registry.URL,
			gcURL:       gc.URL,
			synced:      true,
			wantCode:    http.StatusOK,
			wantStatus:  "ready",
			wantChecks:  map[string]string{"registry": "ok", "ground_control": "ok", "state_sync": "ok"},
		},
		{
			name:        "state sync pending",
			registryURL: registry.URL,
			gcURL:       gc.URL,
			synced:      false,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
			wantChecks:  map[string]string{"registry": "ok", "ground_control": "ok", "state_sync": "pending"},
		},
		{
			name:        "ground control unavailable",
			registryURL: registry.URL,
			gcURL:       unreachable,
			synced:      true,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
			wantChecks:  map[string]string{"registry": "ok", "ground_control": "unavailable", "state_sync": "ok"},
		},
		{
			name:        "registry unavailable",
			registryURL: unreachable,
			gcURL:       gc.URL,
			synced:      true,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
			wantChecks:  map[string]string{"registry": "unavailable", "ground_control": "ok", "state_sync": "ok"},
		},
		{
			name:        "headless skips ground control",
			registryURL: registry.URL,
			gcURL:       unreachable,
			headless:    true,
			synced:      true,
			wantCode:    http.StatusOK,
			wantStatus:  "ready",
			wantChecks:  map[string]string{"registry": "ok", "ground_control": "skipped", "state_sync": "ok"},
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

			if rec.Code != tt.wantCode {
				t.Fatalf("status code: got %d, want %d", rec.Code, tt.wantCode)
			}

			status, checks := decodeReady(t, rec.Body.Bytes())
			if status != tt.wantStatus {
				t.Errorf("status: got %q, want %q", status, tt.wantStatus)
			}
			for k, want := range tt.wantChecks {
				if checks[k] != want {
					t.Errorf("check %q: got %q, want %q", k, checks[k], want)
				}
			}
		})
	}
}

func TestHeadlessDoesNotContactGroundControl(t *testing.T) {
	var gcHit bool
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

	if gcHit {
		t.Error("ground control should not be contacted in headless mode")
	}
}
