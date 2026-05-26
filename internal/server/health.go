package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type HealthRegistrar struct {
	registryURL string
	gcURL       string
	headless    bool
	client      *http.Client
	stateSynced atomic.Bool
}

func NewHealthRegistrar(registryURL, gcURL string, headless bool) *HealthRegistrar {
	return &HealthRegistrar{
		registryURL: strings.TrimRight(registryURL, "/"),
		gcURL:       strings.TrimRight(gcURL, "/"),
		headless:    headless,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// MarkStateSynced records that the initial state sync has completed successfully.
// Safe to call from the state replication goroutine while readyHandler reads it.
func (hr *HealthRegistrar) MarkStateSynced() {
	hr.stateSynced.Store(true)
}

type readyChecks struct {
	Registry      string `json:"registry"`
	GroundControl string `json:"ground_control"`
	StateSync     string `json:"state_sync"`
}

type readyResponse struct {
	Status string      `json:"status"`
	Checks readyChecks `json:"checks"`
}

func (hr *HealthRegistrar) RegisterRoutes(router Router) {
	router.HandleFunc("/health", hr.healthHandler)
	router.HandleFunc("/ready", hr.readyHandler)
}

// healthHandler is a liveness probe: it reports the process is running and must
// not depend on any external dependency.
func (hr *HealthRegistrar) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// readyHandler is a readiness probe: it reports whether the satellite can serve
// its purpose. Registry must be reachable, Ground Control must be reachable
// (unless headless), and the initial state sync must have completed.
func (hr *HealthRegistrar) readyHandler(w http.ResponseWriter, r *http.Request) {
	var checks readyChecks
	ready := true

	if err := hr.checkRegistry(r.Context()); err != nil {
		checks.Registry = "unavailable"
		ready = false
	} else {
		checks.Registry = "ok"
	}

	switch {
	case hr.headless:
		checks.GroundControl = "skipped"
	case hr.checkGroundControl(r.Context()) != nil:
		checks.GroundControl = "unavailable"
		ready = false
	default:
		checks.GroundControl = "ok"
	}

	if hr.stateSynced.Load() {
		checks.StateSync = "ok"
	} else {
		checks.StateSync = "pending"
		ready = false
	}

	status := "ready"
	code := http.StatusOK
	if !ready {
		status = "not_ready"
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(readyResponse{
		Status: status,
		Checks: checks,
	})
}

// checkRegistry pings the local registry's OCI Distribution base endpoint.
// A reachable registry answers /v2/ with 200 (open) or 401 (auth required).
func (hr *HealthRegistrar) checkRegistry(ctx context.Context) error {
	if hr.registryURL == "" {
		return fmt.Errorf("registry URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hr.registryURL+"/v2/", nil)
	if err != nil {
		return fmt.Errorf("create registry health request: %w", err)
	}

	resp, err := hr.client.Do(req)
	if err != nil {
		return fmt.Errorf("send registry health request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return nil
	}

	return fmt.Errorf("registry returned status %d", resp.StatusCode)
}

// checkGroundControl pings Ground Control's /health endpoint, which also
// verifies GC's database. HTTP 200 means GC is reachable and healthy.
func (hr *HealthRegistrar) checkGroundControl(ctx context.Context) error {
	if hr.gcURL == "" {
		return fmt.Errorf("ground control URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hr.gcURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create ground control health request: %w", err)
	}

	resp, err := hr.client.Do(req)
	if err != nil {
		return fmt.Errorf("send ground control health request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	return fmt.Errorf("ground control returned status %d", resp.StatusCode)
}
