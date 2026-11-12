package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

type routeKey struct{ Method, Path string }

// apiRoutes walks the generated router and returns every /api route by
// method and path template.
func apiRoutes(t *testing.T) []routeKey {
	t.Helper()
	router, ok := (&Server{}).RegisterRoutes().(*mux.Router)
	require.True(t, ok, "RegisterRoutes returns the gorilla router")
	var out []routeKey
	require.NoError(t, router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(path, "/api/") {
			return nil
		}
		methods, err := route.GetMethods()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, m := range methods {
			out = append(out, routeKey{m, path})
		}
		return nil
	}))
	require.NotEmpty(t, out)
	return out
}

// concretePath fills a path template with sample values.
func concretePath(template string) string {
	return strings.NewReplacer(
		"{config}", "edge-config",
		"{group}", "g1",
		"{satellite}", "edge-01",
		"{username}", "alice",
		"{id}", "1",
	).Replace(template)
}

// Every /api route of the generated router has exactly one PolicyRoutes
// entry and every entry is a route; the counts come from the router.
func TestPolicyRoutes_CoverGeneratedRouter(t *testing.T) {
	routes := apiRoutes(t)
	entries := map[routeKey]PolicyRoute{}
	for _, e := range PolicyRoutes {
		key := routeKey{e.Method, e.Path}
		require.NotContains(t, entries, key, "%s %s listed twice", e.Method, e.Path)
		entries[key] = e
		require.Contains(t, fleet.ResourceTypes(), e.Type, "%s %s", e.Method, e.Path)
		require.Contains(t, fleet.Actions(), e.Action, "%s %s", e.Method, e.Path)
	}

	for _, r := range routes {
		require.Contains(t, entries, r, "%s %s has no PolicyRoutes entry", r.Method, r.Path)
		require.NotContains(t, concretePath(r.Path), "{", "concretePath does not know a parameter of %s", r.Path)
	}
	require.Len(t, PolicyRoutes, len(routes), "PolicyRoutes lists a route the router does not serve")
}

// The policy API is for system admins only. This runs the builtin engine;
// TestPolicyEquivalence_RouteByRole and
// TestPolicyMiddleware_CasbinMatchesBuiltin cover casbin.
func TestPolicyRoutes_SystemAdminOnly(t *testing.T) {
	var policyAPI []PolicyRoute
	for _, e := range PolicyRoutes {
		if e.Type == fleet.ResourcePolicy {
			policyAPI = append(policyAPI, e)
			require.True(t, requiresSystemAdmin(e.Method, concretePath(e.Path)), "%s %s", e.Method, e.Path)
		}
	}
	require.Len(t, policyAPI, 6)
	require.False(t, requiresSystemAdmin(http.MethodGet, "/api/policiesx"), "a prefix, not a substring")

	h := newGCHarness(t)
	r := h.do(http.MethodPost, "/api/users", CreateUserRequest{Username: "operator", Password: "Operat0rPass"}, h.admin)
	require.Less(t, r.Status, 300, string(r.Body))
	operator := basicAuth("operator", "Operat0rPass")
	for _, e := range policyAPI {
		path := concretePath(e.Path)
		h.mustDo(e.Method, path, nil, nil, http.StatusUnauthorized)
		h.mustDo(e.Method, path, nil, operator, http.StatusForbidden)
		r := h.do(e.Method, path, nil, h.admin)
		require.False(t, slices.Contains([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed}, r.Status), "%s %s: %d %s", e.Method, path, r.Status, r.Body)
	}
}
