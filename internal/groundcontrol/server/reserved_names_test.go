package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

type routeInfo struct {
	template string
	methods  []string
}

func generatedRoutes(t *testing.T) (*mux.Router, []routeInfo) {
	t.Helper()
	router := mux.NewRouter()
	HandlerWithOptions(nil, GorillaServerOptions{BaseRouter: router})
	var routes []routeInfo
	require.NoError(t, router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := route.GetPathTemplate()
		require.NoError(t, err)
		methods, err := route.GetMethods()
		require.NoError(t, err)
		routes = append(routes, routeInfo{template: tpl, methods: methods})
		return nil
	}))
	return router, routes
}

// shadowedNames are, per path parameter, the literal segments of an
// earlier route with the same method and otherwise the same path: gorilla
// matches routes in order, so that parameter can never take those values.
func shadowedNames(routes []routeInfo) map[string][]string {
	out := map[string][]string{}
	for i, lit := range routes {
		ls := strings.Split(lit.template, "/")
		for _, par := range routes[i+1:] {
			ps := strings.Split(par.template, "/")
			if len(ps) != len(ls) || !slices.ContainsFunc(lit.methods, func(m string) bool { return slices.Contains(par.methods, m) }) {
				continue
			}
			diff := -1
			for k := range ls {
				if ls[k] != ps[k] {
					if diff >= 0 {
						diff = -2
						break
					}
					diff = k
				}
			}
			if diff < 0 || !strings.HasPrefix(ps[diff], "{") || strings.HasPrefix(ls[diff], "{") {
				continue
			}
			param := strings.Trim(ps[diff], "{}")
			if !slices.Contains(out[param], ls[diff]) {
				out[param] = append(out[param], ls[diff])
			}
		}
	}
	for k := range out {
		slices.Sort(out[k])
	}
	return out
}

func TestLiteralRoutesShadowParameterValues(t *testing.T) {
	router, routes := generatedRoutes(t)

	require.Equal(t, map[string][]string{
		"satellite": {"active", "stale"},
		"group":     {"satellite"},
	}, shadowedNames(routes))

	for _, tc := range []struct{ method, path, want string }{
		{http.MethodGet, "/api/satellites/active", "/api/satellites/active"},
		{http.MethodGet, "/api/satellites/stale", "/api/satellites/stale"},
		{http.MethodDelete, "/api/satellites/active", "/api/satellites/{satellite}"},
		{http.MethodDelete, "/api/groups/satellite", "/api/groups/satellite"},
		{http.MethodGet, "/api/groups/satellite", "/api/groups/{group}"},
		{http.MethodGet, "/api/groups/sync", "/api/groups/{group}"},
		{http.MethodGet, "/api/satellites/register", "/api/satellites/{satellite}"},
		{http.MethodGet, "/api/configs/satellite", "/api/configs/{config}"},
		{http.MethodGet, "/api/users/password", "/api/users/{username}"},
	} {
		var match mux.RouteMatch
		require.True(t, router.Match(httptest.NewRequest(tc.method, tc.path, nil), &match), "%s %s", tc.method, tc.path)
		require.NoError(t, match.MatchErr)
		got, err := match.Route.GetPathTemplate()
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "%s %s", tc.method, tc.path)
	}
}

func TestReservedNamesMatchRouter(t *testing.T) {
	_, routes := generatedRoutes(t)
	shadowed := shadowedNames(routes)
	require.Equal(t, shadowed["satellite"], fleet.ReservedSatelliteNames)
	require.Equal(t, shadowed["group"], fleet.ReservedGroupNames)
	delete(shadowed, "satellite")
	delete(shadowed, "group")
	require.Empty(t, shadowed, "a new collision needs its names reserved at creation")
}

func TestCreateWithReservedNameIs400(t *testing.T) {
	s, repo := fakeFleetServer(t)
	repo.Configs.ByName = map[string]fleet.Config{"edge-config": {ID: 1, Name: "edge-config"}}
	post := func(h http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h(rr, req)
		return rr
	}
	for _, name := range fleet.ReservedSatelliteNames {
		rr := post(s.RegisterSatellite, "/api/satellites", `{"name":"`+name+`","config_name":"edge-config"}`)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Contains(t, rr.Body.String(), "reserved")
	}
	for _, name := range fleet.ReservedGroupNames {
		rr := post(s.SyncGroup, "/api/groups/sync", `{"group":"`+name+`","artifacts":[]}`)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Contains(t, rr.Body.String(), "reserved")
	}
}
