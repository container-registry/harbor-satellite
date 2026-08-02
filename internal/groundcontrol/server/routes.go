package server

import (
	"net/http"
	"path"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	router := mux.NewRouter()
	router.Use(s.RequestIDMiddleware)
	router.Use(s.routeSecurityMiddleware)

<<<<<<< HEAD
	return HandlerWithOptions(s, GorillaServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteJSONError(w, err.Error(), http.StatusBadRequest)
		},
	})
}

// routeSecurityMiddleware retains the endpoint-specific policy that used to
// live in the handwritten route registrations while generated code owns route
// matching and parameter extraction.
func (s *Server) routeSecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler := next
		cleanPath := path.Clean(r.URL.Path)

		switch cleanPath {
		case "/login", "/satellites/ztr":
			handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
		case "/satellites/spiffe-ztr":
			handler = spiffe.RequireSPIFFEAuth(handler)
			handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
		case "/satellites/sync":
			handler = spiffe.AuthMiddleware(handler)
			handler = s.SatelliteAuthMiddleware(handler)
			handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
		}

		if strings.HasPrefix(cleanPath, "/api/") {
			if requiresSystemAdmin(r.Method, cleanPath) {
				handler = s.RequireRole(roleSystemAdmin, handler.ServeHTTP)
			}
			handler = s.AuthMiddleware(handler)
		}

		handler.ServeHTTP(w, r)
	})
}

func requiresSystemAdmin(method, requestPath string) bool {
	switch {
	case method == http.MethodPost && requestPath == "/api/users":
		return true
	case method == http.MethodDelete && strings.HasPrefix(requestPath, "/api/users/"):
		return true
	case method == http.MethodPatch && strings.HasPrefix(requestPath, "/api/users/") && strings.HasSuffix(requestPath, "/password") && requestPath != "/api/users/password":
		return true
	case method == http.MethodDelete && strings.HasPrefix(requestPath, "/api/groups/") && requestPath != "/api/groups/satellite":
		return true
	case strings.HasPrefix(requestPath, "/api/spire/"):
		return true
	case method == http.MethodPost && requestPath == "/api/satellites/register":
		return true
	default:
		return false
	}
=======
	// Attach a request ID to every request (all routes, including /login and
	// /ztr) so the audit events from one request share a correlation ID.
	r.Use(s.RequestIDMiddleware)

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

	// Auth
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
	api.HandleFunc("/satellites/{satellite}/images", s.getCachedImagesHandler).Methods("GET")

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
	satEndpoints := r.PathPrefix("/sat").Subrouter()
	satEndpoints.Use(middleware.RateLimitMiddleware(s.rateLimiter))
	satEndpoints.Use(spiffe.AuthMiddleware)
	satEndpoints.Use(s.SatelliteAuthMiddleware)

	satEndpoints.HandleFunc("/sync", s.syncHandler).Methods("POST")
	satEndpoints.HandleFunc("/refresh", s.refreshCredentialsHandler).Methods("POST")

	return r
>>>>>>> 55b5a79 (feat: initial implementation)
}
