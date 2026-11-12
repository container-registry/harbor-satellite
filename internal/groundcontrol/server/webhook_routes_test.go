package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

func TestWebhookRoutes_SystemAdminOnly(t *testing.T) {
	var routes []PolicyRoute
	for _, e := range PolicyRoutes {
		if e.Type == fleet.ResourceWebhook {
			routes = append(routes, e)
			require.True(t, requiresSystemAdmin(e.Method, concretePath(e.Path)), "%s %s", e.Method, e.Path)
		}
	}
	require.Len(t, routes, 3)
	require.False(t, requiresSystemAdmin(http.MethodGet, "/api/webhooksx"), "a prefix, not a substring")

	h := newGCHarness(t)
	r := h.do(http.MethodPost, "/api/users", CreateUserRequest{Username: "operator", Password: "Operat0rPass"}, h.admin)
	require.Less(t, r.Status, 300, string(r.Body))
	operator := basicAuth("operator", "Operat0rPass")
	for _, e := range routes {
		path := concretePath(e.Path)
		h.mustDo(e.Method, path, nil, nil, http.StatusUnauthorized)
		h.mustDo(e.Method, path, nil, operator, http.StatusForbidden)
		r := h.do(e.Method, path, nil, h.admin)
		require.False(t, slices.Contains([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed}, r.Status), "%s %s: %d %s", e.Method, path, r.Status, r.Body)
	}
}
