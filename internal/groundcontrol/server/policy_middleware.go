package server

import "net/http"

// PolicyMiddleware is the route check of plan item 7.2, used instead of
// requiresSystemAdmin when POLICY_ENGINE is casbin. It finds the
// PolicyRoutes entry of the matched route (mux.CurrentRoute's path
// template and the method) and asks the policy's AuthorizeType for the
// principal AuthMiddleware stored: 403 when denied or when the route has no
// entry, 500 when the policy fails. Resource scopes are checked in the
// services. Stub: it answers 501.
func (s *Server) PolicyMiddleware(_ http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		notImplemented(w, "7.2 policy route check")
	})
}
