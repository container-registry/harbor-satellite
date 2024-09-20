package satellite

import (
	"encoding/json"
	"net/http"

	"container-registry.com/harbor-satellite/internal/server"
)

type SatelliteRegistrar struct{}

type SatelliteResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code"`
}

func (sr *SatelliteRegistrar) RegisterRoutes(router server.Router) {
	satelliteGroup := router.Group("/satellite")
	satelliteGroup.HandleFunc("/ping", sr.Ping)
}

func (sr *SatelliteRegistrar) Ping(w http.ResponseWriter, r *http.Request) {
	response := SatelliteResponse{
		Success:    true,
		Message:    "Ping satellite successful",
		StatusCode: http.StatusOK,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
