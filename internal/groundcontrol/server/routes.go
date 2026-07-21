package server

import (
	"net/http"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/gorilla/mux"
)

func (s *Server) RegisterRoutes() http.Handler {
	router := mux.NewRouter()
	router.Use(s.RequestIDMiddleware)
	router.Use(s.routeSecurityMiddleware)

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

		switch r.URL.Path {
		case "/login", "/satellites/ztr":
			handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
		case "/satellites/spiffe-ztr":
			handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
			handler = spiffe.RequireSPIFFEAuth(handler)
		case "/satellites/sync":
			handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
			handler = spiffe.AuthMiddleware(handler)
			handler = s.SatelliteAuthMiddleware(handler)
		}

		if strings.HasPrefix(r.URL.Path, "/api/") {
			if requiresSystemAdmin(r.Method, r.URL.Path) {
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
	case method == http.MethodPatch && strings.HasPrefix(requestPath, "/api/users/") && strings.HasSuffix(requestPath, "/password"):
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
}
