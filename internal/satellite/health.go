package satellite

import (
	"encoding/json"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/logger"
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
	log := logger.FromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	payload := HealthResponse{
		Status:  "healthy",
		Version: version.Version,
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Error().Err(err).Msg("failed to encode health response")
		// If headers were already sent, this will only log a warning in the server,
		// but it's better than ignoring the error.
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}
