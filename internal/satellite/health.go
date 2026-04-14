package satellite

import (
	"encoding/json"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/server"
	"github.com/container-registry/harbor-satellite/internal/version"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type HealthRegistrar struct{}

func (h *HealthRegistrar) RegisterRoutes(router server.Router) {
	router.HandleFunc("/health", h.HealthHandler)
}

func (h *HealthRegistrar) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(HealthResponse{
		Status:  "healthy",
		Version: version.Version,
	})
}
