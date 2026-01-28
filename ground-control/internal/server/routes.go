package server

import (
	"net/http"

	"github.com/container-registry/harbor-satellite/ground-control/internal/middleware"
	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")

	// Groups
	r.HandleFunc("/groups", s.listGroupHandler).Methods("GET")        // List all groups
	r.HandleFunc("/groups/sync", s.groupsSyncHandler).Methods("POST") // Sync groups
	r.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET") // Get specific group

	// Satellites in groups
	r.HandleFunc("/groups/{group}/satellites", s.groupSatelliteHandler).Methods("GET") // List satellites in group
	r.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")           // Add satellite to group
	r.HandleFunc("/groups/satellite", s.removeSatelliteFromGroup).Methods("DELETE")    // Remove satellite from group

	// Configs
	r.HandleFunc("/configs", s.listConfigsHandler).Methods("GET")
	r.HandleFunc("/configs", s.createConfigHandler).Methods("POST")
	r.HandleFunc("/configs/{config}", s.updateConfigHandler).Methods("PATCH")
	r.HandleFunc("/configs/{config}", s.getConfigHandler).Methods("GET")
	r.HandleFunc("/configs/{config}", s.deleteConfigHandler).Methods("DELETE")
	r.HandleFunc("/configs/satellite", s.setSatelliteConfig).Methods("POST")

	// ZTR endpoints (must be defined before wildcard routes)
	// Token-based ZTR (backward compatible)
	ztrSubrouter := r.PathPrefix("/satellites/ztr").Subrouter()
	ztrSubrouter.Use(middleware.RateLimitMiddleware(s.rateLimiter))
	ztrSubrouter.HandleFunc("/{token}", s.ztrHandler).Methods("GET")

	// SPIFFE-based ZTR (mTLS authentication, no token required)
	// The satellite's identity is verified via SPIFFE SVID in TLS handshake
	spiffeZtrSubrouter := r.PathPrefix("/satellites/spiffe-ztr").Subrouter()
	spiffeZtrSubrouter.Use(spiffe.RequireSPIFFEAuth)
	spiffeZtrSubrouter.Use(middleware.RateLimitMiddleware(s.rateLimiter))
	spiffeZtrSubrouter.HandleFunc("", s.spiffeZtrHandler).Methods("GET")

	// SPIRE status endpoint
	r.HandleFunc("/spire/status", s.spireStatusHandler).Methods("GET")

	// Satellites (wildcard routes must come after specific routes)
	r.HandleFunc("/satellites", s.listSatelliteHandler).Methods("GET")      // List all satellites
	r.HandleFunc("/satellites", s.registerSatelliteHandler).Methods("POST") // Register new satellite
	r.HandleFunc("/satellites/{satellite}/join-token", s.generateJoinTokenHandler).Methods("POST") // SPIRE join token
	r.HandleFunc("/satellites/{satellite}", s.GetSatelliteByName).Methods("GET")       // Get specific satellite
	r.HandleFunc("/satellites/{satellite}", s.DeleteSatelliteByName).Methods("DELETE") // Delete specific satellite

	return r
}
