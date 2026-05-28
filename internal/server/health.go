package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// RegistryURLFunc returns the local registry URL, resolved per probe so
// hot-reloads of zot config are picked up without a restart.
type RegistryURLFunc func() (string, error)

type HealthRegistrar struct {
	registryURL RegistryURLFunc
	gcURL       string
	headless    bool
	client      *http.Client
	gcClient    func(context.Context) (*http.Client, error)
	stateSynced atomic.Bool
}

// NewHealthRegistrar builds the registrar. client is the static client (registry
// check, and Ground Control when gcClient is nil); pass nil for a bare default
// gcClient, when set, supplies the Ground Control client — used for SPIFFE mTLS
func NewHealthRegistrar(
	registryURL RegistryURLFunc,
	gcURL string,
	headless bool,
	client *http.Client,
	gcClient func(context.Context) (*http.Client, error),
) *HealthRegistrar {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &HealthRegistrar{
		registryURL: registryURL,
		gcURL:       strings.TrimRight(gcURL, "/"),
		headless:    headless,
		client:      client,
		gcClient:    gcClient,
	}
}

// MarkStateSynced records a successful initial state sync. Safe to call
// concurrently with readyHandler reads (backed by atomic.Bool).
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

// healthHandler is the liveness probe — always 200, no dependency checks.
func (hr *HealthRegistrar) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// readyHandler is the readiness probe — 200 only if registry is up, GC is up
// (or headless), and the initial state sync has completed.
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

// checkRegistry pings the local registry's /v2/ endpoint; 200 or 401 means up.
func (hr *HealthRegistrar) checkRegistry(ctx context.Context) error {
	if hr.registryURL == nil {
		return errors.New("registry URL provider is not configured")
	}
	url, err := hr.registryURL()
	if err != nil {
		return fmt.Errorf("resolve registry URL: %w", err)
	}
	if url == "" {
		return errors.New("registry URL is empty")
	}
	url = strings.TrimRight(url, "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/v2/", nil)
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

// checkGroundControl pings GC's /health (which also covers GC's DB); 200 = up.
func (hr *HealthRegistrar) checkGroundControl(ctx context.Context) error {
	if hr.gcURL == "" {
		return errors.New("ground control URL is empty")
	}

	client := hr.client
	if hr.gcClient != nil {
		c, err := hr.gcClient(ctx)
		if err != nil {
			return fmt.Errorf("build ground control client: %w", err)
		}
		if c == nil {
			return errors.New("ground control client provider returned nil client without error")
		}
		client = c
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hr.gcURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create ground control health request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send ground control health request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	return fmt.Errorf("ground control returned status %d", resp.StatusCode)
}
