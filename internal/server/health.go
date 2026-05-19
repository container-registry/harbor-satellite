package server

import "net/http"

type HealthRegistrar struct{}

func (hr *HealthRegistrar) RegisterRoutes(router Router) {
	router.HandleFunc("GET /health", hr.healthHandler)
}

func (hr *HealthRegistrar) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
