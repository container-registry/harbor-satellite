package server

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	// Public routes
	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")
	r.HandleFunc("/login", s.loginHandler).Methods("POST")
	r.HandleFunc("/satellites/ztr/{token}", s.ztrHandler).Methods("GET") // Satellite token auth

	// Protected routes (require authentication)
	protected := r.PathPrefix("").Subrouter()
	protected.Use(s.AuthMiddleware)

	// Auth
	protected.HandleFunc("/logout", s.logoutHandler).Methods("POST")

	// Users (all authenticated users)
	protected.HandleFunc("/users", s.listUsersHandler).Methods("GET")
	protected.HandleFunc("/users/password", s.changeOwnPasswordHandler).Methods("PATCH")
	protected.HandleFunc("/users/{username}", s.getUserHandler).Methods("GET")

	// User management (system_admin only)
	protected.HandleFunc("/users", s.RequireRole(roleSystemAdmin, s.createUserHandler)).Methods("POST")
	protected.HandleFunc("/users/{username}", s.RequireRole(roleSystemAdmin, s.deleteUserHandler)).Methods("DELETE")
	protected.HandleFunc("/users/{username}/password", s.RequireRole(roleSystemAdmin, s.changeUserPasswordHandler)).Methods("PATCH")

	// Groups
	protected.HandleFunc("/groups", s.listGroupHandler).Methods("GET")
	protected.HandleFunc("/groups/sync", s.groupsSyncHandler).Methods("POST")
	protected.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET")

	// Satellites in groups
	protected.HandleFunc("/groups/{group}/satellites", s.groupSatelliteHandler).Methods("GET")
	protected.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")
	protected.HandleFunc("/groups/satellite", s.removeSatelliteFromGroup).Methods("DELETE")

	// Configs
	protected.HandleFunc("/configs", s.listConfigsHandler).Methods("GET")
	protected.HandleFunc("/configs", s.createConfigHandler).Methods("POST")
	protected.HandleFunc("/configs/{config}", s.updateConfigHandler).Methods("PATCH")
	protected.HandleFunc("/configs/{config}", s.getConfigHandler).Methods("GET")
	protected.HandleFunc("/configs/{config}", s.deleteConfigHandler).Methods("DELETE")
	protected.HandleFunc("/configs/satellite", s.setSatelliteConfig).Methods("POST")

	// Satellites
	protected.HandleFunc("/satellites", s.listSatelliteHandler).Methods("GET")
	protected.HandleFunc("/satellites", s.registerSatelliteHandler).Methods("POST")
	protected.HandleFunc("/satellites/sync", s.syncHandler).Methods("POST")
	protected.HandleFunc("/satellites/active", s.getActiveSatellitesHandler).Methods("GET")
	protected.HandleFunc("/satellites/stale", s.getStaleSatellitesHandler).Methods("GET")
	protected.HandleFunc("/satellites/{satellite}", s.GetSatelliteByName).Methods("GET")
	protected.HandleFunc("/satellites/{satellite}", s.DeleteSatelliteByName).Methods("DELETE")
	protected.HandleFunc("/satellites/{satellite}/status", s.getSatelliteStatusHandler).Methods("GET")

	return r
}
