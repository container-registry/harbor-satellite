package server

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

type HealthRegistrar struct {
	registryURL string
	gcURL       string
	headless    bool
	syncDone    *atomic.Bool
}

func NewHealthRegistrar(registryURL, gcURL string, headless bool, syncDone *atomic.Bool) *HealthRegistrar {
	return &HealthRegistrar{
		registryURL: registryURL,
		gcURL:       gcURL,
		headless:    headless,
		syncDone:    syncDone,
	}
}

func (h *HealthRegistrar) RegisterRoutes(r Router) {
	r.HandleFunc("GET /health", h.handleHealth)
	r.HandleFunc("GET /ready", h.handleReady)
}

func (h *HealthRegistrar) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *HealthRegistrar) handleReady(w http.ResponseWriter, _ *http.Request) {
	checks := map[string]string{}
	allOK := true

	resp, err := http.Get(h.registryURL + "/v2/")
	if err != nil || (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized) {
		checks["registry"] = "unavailable"
		allOK = false
	} else {
		checks["registry"] = "ok"
	}

	if h.headless {
		checks["ground_control"] = "skipped"
	} else {
		resp, err := http.Get(h.gcURL + "/ping")
		if err != nil || resp.StatusCode != http.StatusOK {
			checks["ground_control"] = "unavailable"
			allOK = false
		} else {
			checks["ground_control"] = "ok"
		}
	}

	if h.syncDone.Load() {
		checks["state_sync"] = "ok"
	} else {
		checks["state_sync"] = "pending"
		allOK = false
	}

	status, code := "ready", http.StatusOK
	if !allOK {
		status, code = "not ready", http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"status": status, "checks": checks})
}
