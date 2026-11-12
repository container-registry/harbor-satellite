package server

import (
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/gorilla/mux"
)

// PolicyMiddleware is the route check of plan item 7.2, used instead of
// requiresSystemAdmin when POLICY_ENGINE is casbin. It finds the
// PolicyRoutes entry of the matched route (mux.CurrentRoute's path
// template and the method) and asks the policy's AuthorizeType for the
// principal AuthMiddleware stored: 403 when denied or when the route has no
// entry, 500 when the policy fails. Resource scopes are checked in the
// services.
func (s *Server) PolicyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entry, ok := policyRouteOf(r)
		if !ok {
			WriteJSONError(w, "Forbidden", http.StatusForbidden)
			return
		}
		principal, _ := fleet.PrincipalFrom(r.Context())
		d, err := s.policy.AuthorizeType(r.Context(), principal, entry.Action, entry.Type)
		if err != nil {
			log.Printf("policy route check %s %s: %v", entry.Method, entry.Path, err)
			WriteJSONError(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !d.Allowed {
			WriteJSONError(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func policyRouteOf(r *http.Request) (PolicyRoute, bool) {
	route := mux.CurrentRoute(r)
	if route == nil {
		return PolicyRoute{}, false
	}
	tmpl, err := route.GetPathTemplate()
	if err != nil {
		return PolicyRoute{}, false
	}
	for _, e := range PolicyRoutes {
		if e.Method == r.Method && e.Path == tmpl {
			return e, true
		}
	}
	return PolicyRoute{}, false
}
