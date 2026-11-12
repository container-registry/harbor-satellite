package server

import (
	"net/http"
	"path"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/gorilla/mux"
)

// anonymousMaxBodyBytes bounds what a request without credentials can make
// Ground Control read; login, ZTR and the OIDC device poll send a few hundred
// bytes.
const anonymousMaxBodyBytes = 64 << 10

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
		cleanPath := path.Clean(r.URL.Path)

		switch cleanPath {
		case "/login", "/oidc/login", "/oidc/callback", "/oidc/device", "/oidc/device/token", "/satellites/ztr", "/satellites/spiffe-ztr":
			r.Body = http.MaxBytesReader(w, r.Body, anonymousMaxBodyBytes)
		}

		switch cleanPath {
		case "/login":
			// With RATE_LIMIT_SPLIT the FailureLimiter alone guards /login.
			if s.AnonymousLimiter == nil {
				handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
			}
		case "/oidc/login", "/oidc/callback", "/oidc/device", "/oidc/device/token":
			// Anonymous with or without RATE_LIMIT_SPLIT; /oidc/login also
			// stores a login state per request.
			handler = s.limitAnonymous(handler)
		case "/satellites/ztr":
			handler = s.limitAnonymous(handler)
		case "/satellites/spiffe-ztr":
			handler = spiffe.RequireSPIFFEAuth(handler)
			handler = s.limitAnonymous(handler)
		case "/satellites/sync", "/satellites/refresh":
			handler = s.satelliteAuthChain(handler)
		}

		if strings.HasPrefix(cleanPath, "/api/") {
			if s.policyRouteCheck() {
				handler = s.PolicyMiddleware(handler)
			} else if requiresSystemAdmin(r.Method, cleanPath) {
				handler = s.RequireRole(roleSystemAdmin, handler.ServeHTTP)
			}
			handler = s.AuthMiddleware(handler)
		}

		handler.ServeHTTP(w, r)
	})
}

// policyRouteCheck reports whether the /api routes are checked by the
// policy (PolicyMiddleware) rather than by requiresSystemAdmin.
func (s *Server) policyRouteCheck() bool {
	return s.policy != nil && s.policyEngine == env.PolicyEngineCasbin
}

// satelliteAuthChain authenticates satellite-facing endpoints. The SPIFFE
// middleware is the outer layer so an SVID is in the context before
// SatelliteAuthMiddleware runs and wins over Basic credentials. With a
// SatelliteAuthenticator (plan item 4.1) the chain is the SPIFFE
// middleware, then authenticateSatellite, then the per-satellite fleet
// limiter; the shared pre-auth limiter stays only while there is no fleet
// limiter.
func (s *Server) satelliteAuthChain(next http.Handler) http.Handler {
	if s.satelliteAuth != nil {
		handler := spiffe.AuthMiddleware(s.authenticateSatellite(s.fleetLimit(next)))
		if s.FleetLimiter == nil {
			handler = middleware.RateLimitMiddleware(s.rateLimiter)(handler)
		}
		return handler
	}
	handler := s.SatelliteAuthMiddleware(next)
	handler = spiffe.AuthMiddleware(handler)
	return middleware.RateLimitMiddleware(s.rateLimiter)(handler)
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
	case strings.HasPrefix(requestPath, "/api/ops/"):
		return true
	case requestPath == "/api/policies" || strings.HasPrefix(requestPath, "/api/policies/"):
		return true
	case requestPath == "/api/role-bindings" || strings.HasPrefix(requestPath, "/api/role-bindings/"):
		return true
	case requestPath == "/api/webhooks" || strings.HasPrefix(requestPath, "/api/webhooks/"):
		return true
	case method == http.MethodPost && requestPath == "/api/satellites/register":
		return true
	default:
		return false
	}
}
