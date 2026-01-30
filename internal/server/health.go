package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

type HealthRegistrar struct {
	cm     *config.ConfigManager
	logger *zerolog.Logger
}

type HealthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func NewHealthRegistrar(cm *config.ConfigManager, logger *zerolog.Logger) *HealthRegistrar {
	return &HealthRegistrar{
		cm:     cm,
		logger: logger,
	}
}

func (h *HealthRegistrar) RegisterRoutes(router Router) {
	router.HandleFunc("/health", h.healthHandler)
	router.HandleFunc("/ready", h.readyHandler)
}

func (h *HealthRegistrar) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(HealthResponse{Status: "ok"}); err != nil {
		h.logger.Error().Err(err).Msg("failed to write health response")
	}
}

func (h *HealthRegistrar) readyHandler(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]string)

	// Check registry
	if err := h.checkRegistry(); err != nil {
		checks["registry"] = fmt.Sprintf("error: %v", err)
	} else {
		checks["registry"] = "ok"
	}

	// Check ground control
	if err := h.checkGroundControl(); err != nil {
		checks["ground_control"] = fmt.Sprintf("error: %v", err)
	} else {
		checks["ground_control"] = "ok"
	}

	// Check state sync
	if !h.cm.IsZTRDone() {
		checks["state_sync"] = "not ready: initial state sync not complete"
	} else {
		checks["state_sync"] = "ok"
	}

	// Determine overall status
	status := "ready"
	for _, check := range checks {
		if check != "ok" {
			status = "not ready"
			break
		}
	}

	response := HealthResponse{
		Status: status,
		Checks: checks,
	}

	w.Header().Set("Content-Type", "application/json")
	if status == "ready" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("failed to write rediness response")
	}
}

func (h *HealthRegistrar) checkRegistry() error {
	// Get local registry endpoint
	// From main.go, it's constructed from zot config
	var data map[string]interface{}
	if err := json.Unmarshal(h.cm.GetRawZotConfig(), &data); err != nil {
		return fmt.Errorf("failed to parse zot config: %w", err)
	}
	httpData, ok := data["http"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid zot config: missing http section")
	}
	address, ok := httpData["address"].(string)
	if !ok {
		return fmt.Errorf("invalid zot config: missing address")
	}
	port, ok := httpData["port"].(string)
	if !ok {
		return fmt.Errorf("invalid zot config: missing port")
	}
	registryURL := fmt.Sprintf("http://%s:%s/v2/", address, port)

	// Ping the registry
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(registryURL)
	if err != nil {
		return fmt.Errorf("registry not accessible: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.logger.Warn().Err(err).Msg("failed to close registry response body")
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	return nil
}

func (h *HealthRegistrar) checkGroundControl() error {
	gcURL := h.cm.ResolveGroundControlURL()

	// Simple ping to GC
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(gcURL + "/ping")
	if err != nil {
		return fmt.Errorf("ground control not accessible: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.logger.Warn().Err(err).Msg("failed to close ground control response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ground control returned status %d", resp.StatusCode)
	}

	return nil
}
