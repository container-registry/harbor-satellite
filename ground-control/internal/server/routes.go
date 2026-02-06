package server

import (
	"net/http"

	"github.com/container-registry/harbor-satellite/ground-control/internal/middleware"
	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := mux.NewRouter()

	// Public routes
	r.HandleFunc("/ping", s.Ping).Methods("GET")
	r.HandleFunc("/health", s.healthHandler).Methods("GET")

	// Login (rate limited, public)
	loginRouter := r.PathPrefix("/login").Subrouter()
	loginRouter.Use(middleware.RateLimitMiddleware(s.rateLimiter))
	loginRouter.HandleFunc("", s.loginHandler).Methods("POST")

	// Human API routes (user auth required)
	api := r.PathPrefix("/api").Subrouter()
	api.Use(s.AuthMiddleware)

	// Logout
	api.HandleFunc("/logout", s.logoutHandler).Methods("POST")

	// Users
	api.HandleFunc("/users", s.listUsersHandler).Methods("GET")
	api.HandleFunc("/users/password", s.changeOwnPasswordHandler).Methods("PATCH")
	api.HandleFunc("/users/{username}", s.getUserHandler).Methods("GET")
	api.HandleFunc("/users", s.RequireRole(roleSystemAdmin, s.createUserHandler)).Methods("POST")
	api.HandleFunc("/users/{username}", s.RequireRole(roleSystemAdmin, s.deleteUserHandler)).Methods("DELETE")
	api.HandleFunc("/users/{username}/password", s.RequireRole(roleSystemAdmin, s.changeUserPasswordHandler)).Methods("PATCH")

	// Groups
	api.HandleFunc("/groups", s.listGroupHandler).Methods("GET")
	api.HandleFunc("/groups/sync", s.groupsSyncHandler).Methods("POST")
	api.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET")
	api.HandleFunc("/groups/{group}/satellites", s.groupSatelliteHandler).Methods("GET")
	api.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")
	api.HandleFunc("/groups/satellite", s.removeSatelliteFromGroup).Methods("DELETE")
	api.HandleFunc("/groups/{group}", s.RequireRole(roleSystemAdmin, s.deleteGroupHandler)).Methods("DELETE")

	// Configs
	api.HandleFunc("/configs", s.listConfigsHandler).Methods("GET")
	api.HandleFunc("/configs", s.createConfigHandler).Methods("POST")
	api.HandleFunc("/configs/{config}", s.updateConfigHandler).Methods("PATCH")
	api.HandleFunc("/configs/{config}", s.getConfigHandler).Methods("GET")
	api.HandleFunc("/configs/{config}", s.deleteConfigHandler).Methods("DELETE")
	api.HandleFunc("/configs/satellite", s.setSatelliteConfig).Methods("POST")

	// Satellite management (human only)
	api.HandleFunc("/satellites", s.listSatelliteHandler).Methods("GET")
	api.HandleFunc("/satellites", s.registerSatelliteHandler).Methods("POST")
	api.HandleFunc("/satellites/active", s.getActiveSatellitesHandler).Methods("GET")
	api.HandleFunc("/satellites/stale", s.getStaleSatellitesHandler).Methods("GET")
	api.HandleFunc("/satellites/{satellite}", s.GetSatelliteByName).Methods("GET")
	api.HandleFunc("/satellites/{satellite}", s.DeleteSatelliteByName).Methods("DELETE")
	api.HandleFunc("/satellites/{satellite}/status", s.getSatelliteStatusHandler).Methods("GET")

	// SPIRE management (admin only)
	api.HandleFunc("/spire/status", s.RequireRole(roleSystemAdmin, s.spireStatusHandler)).Methods("GET")
	api.HandleFunc("/spire/agents", s.RequireRole(roleSystemAdmin, s.listSpireAgentsHandler)).Methods("GET")
	api.HandleFunc("/satellites/register", s.RequireRole(roleSystemAdmin, s.registerSatelliteWithSPIFFEHandler)).Methods("POST")

	// Satellite routes (robot creds or SPIFFE)
	satellites := r.PathPrefix("/satellites").Subrouter()

	// Token-based ZTR (rate limited)
	ztr := satellites.PathPrefix("/ztr").Subrouter()
	ztr.Use(middleware.RateLimitMiddleware(s.rateLimiter))
	ztr.HandleFunc("/{token}", s.ztrHandler).Methods("GET")

	// SPIFFE-based ZTR (rate limited)
	spiffeZtr := satellites.PathPrefix("/spiffe-ztr").Subrouter()
	spiffeZtr.Use(spiffe.RequireSPIFFEAuth)
	spiffeZtr.Use(middleware.RateLimitMiddleware(s.rateLimiter))
	spiffeZtr.HandleFunc("", s.spiffeZtrHandler).Methods("GET")

	// Sync (dual auth: robot credentials or SPIFFE)
	satellites.HandleFunc("/sync", s.syncHandler).Methods("POST")

	return r
}
