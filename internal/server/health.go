package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

// HealthResponse represents the health check response structure
type HealthResponse struct {
	Status          string    `json:"status"`
	SatelliteStatus string    `json:"satellite_status"`
	RegistryStatus  string    `json:"registry_status"`
	Timestamp       time.Time `json:"timestamp"`
}

// HealthRegistrar handles health check endpoints
type HealthRegistrar struct {
	cm *config.ConfigManager
}

// NewHealthRegistrar creates a new health check registrar
func NewHealthRegistrar(cm *config.ConfigManager) *HealthRegistrar {
	return &HealthRegistrar{cm: cm}
}

// RegisterRoutes registers the health check endpoint
func (h *HealthRegistrar) RegisterRoutes(router Router) {
	router.HandleFunc("GET /health", h.healthHandler)
}

// healthHandler handles GET /health requests
func (h *HealthRegistrar) healthHandler(w http.ResponseWriter, r *http.Request) {
	response := h.checkHealth()

	w.Header().Set("Content-Type", "application/json")

	// Set status code based on overall health
	if response.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// checkHealth performs health checks on satellite and registry
func (h *HealthRegistrar) checkHealth() HealthResponse {
	response := HealthResponse{
		Timestamp: time.Now(),
	}

	// Check satellite registration status
	if h.cm.IsZTRDone() {
		response.SatelliteStatus = "registered"
	} else {
		response.SatelliteStatus = "not_registered"
	}

	// Check registry configuration
	if h.cm.GetOwnRegistry() {
		response.RegistryStatus = "external"
	} else if h.cm.GetZotURL() != "" {
		response.RegistryStatus = "running"
	} else {
		response.RegistryStatus = "not_configured"
	}

	// Determine overall status
	if response.SatelliteStatus == "registered" && 
	   (response.RegistryStatus == "running" || response.RegistryStatus == "external") {
		response.Status = "healthy"
	} else {
		response.Status = "degraded"
	}

	return response
}