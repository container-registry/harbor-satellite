package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

type routeClass string

const (
	classPublic    routeClass = "public"
	classAnonymous routeClass = "anonymous, rate limited"
	classSatellite routeClass = "satellite credentials"
)

// nonAPIRoutes is every route outside /api and how it is guarded. A new
// route outside /api needs an entry here and a case in
// routeSecurityMiddleware, or it is served without authentication.
var nonAPIRoutes = map[routeKey]routeClass{
	{http.MethodGet, "/ping"}:                  classPublic,
	{http.MethodGet, "/health"}:                classPublic,
	{http.MethodPost, "/login"}:                classAnonymous,
	{http.MethodGet, "/oidc/login"}:            classAnonymous,
	{http.MethodGet, "/oidc/callback"}:         classAnonymous,
	{http.MethodPost, "/oidc/device"}:          classAnonymous,
	{http.MethodPost, "/oidc/device/token"}:    classAnonymous,
	{http.MethodPost, "/satellites/ztr"}:       classAnonymous,
	{http.MethodGet, "/satellites/spiffe-ztr"}: classAnonymous,
	{http.MethodPost, "/satellites/sync"}:      classSatellite,
	{http.MethodPost, "/satellites/refresh"}:   classSatellite,
}

func allRoutes(t *testing.T) []routeKey {
	t.Helper()
	router, ok := (&Server{}).RegisterRoutes().(*mux.Router)
	require.True(t, ok)
	var out []routeKey
	require.NoError(t, router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil {
			return err
		}
		methods, err := route.GetMethods()
		if err != nil {
			return err
		}
		for _, m := range methods {
			out = append(out, routeKey{m, path})
		}
		return nil
	}))
	return out
}

func TestRouteSecurity_EveryRouteIsClassified(t *testing.T) {
	seen := map[routeKey]bool{}
	for _, r := range allRoutes(t) {
		if strings.HasPrefix(r.Path, "/api/") {
			continue
		}
		require.Contains(t, nonAPIRoutes, r, "%s %s is outside /api and not classified", r.Method, r.Path)
		seen[r] = true
	}
	for r := range nonAPIRoutes {
		require.True(t, seen[r], "%s %s is classified but not served", r.Method, r.Path)
	}
}

// Without valid credentials every /api route and every satellite route
// answers 401 before any handler runs, for each way a request can carry
// credentials. The failure limiter turns repeated failures into 429, which
// is a rejection too.
func TestRouteSecurity_RejectsMissingAndInvalidCredentials(t *testing.T) {
	h := newGCHarness(t)
	invalid := map[string]authFunc{
		"none":                 nil,
		"wrong password":       basicAuth("admin", "not-the-password"),
		"unknown user":         basicAuth("nobody", harnessAdminPassword),
		"unknown bearer":       bearerAuth("hss_" + strings.Repeat("A", 43)),
		"malformed bearer":     bearerAuth("not-a-token"),
		"empty bearer":         func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") },
		"unsupported scheme":   func(r *http.Request) { r.Header.Set("Authorization", "Digest abc") },
		"basic without colon":  func(r *http.Request) { r.Header.Set("Authorization", "Basic YWRtaW4=") },
		"basic not base64":     func(r *http.Request) { r.Header.Set("Authorization", "Basic %%%") },
		"empty basic password": basicAuth("admin", ""),
	}

	var guarded []routeKey
	for _, r := range allRoutes(t) {
		if strings.HasPrefix(r.Path, "/api/") || nonAPIRoutes[r] == classSatellite {
			guarded = append(guarded, r)
		}
	}
	require.NotEmpty(t, guarded)

	unauthorized := 0
	for name, auth := range invalid {
		for _, r := range guarded {
			got := h.do(r.Method, concretePath(r.Path), nil, auth)
			require.Contains(t, []int{http.StatusUnauthorized, http.StatusTooManyRequests}, got.Status, "%s %s with %s: %s", r.Method, r.Path, name, got.Body)
			if got.Status == http.StatusUnauthorized {
				unauthorized++
			}
		}
	}
	require.Positive(t, unauthorized)
}

// A satellite's robot secret and session token open the satellite routes
// only, and an operator's credentials open /api only.
func TestRouteSecurity_CredentialsDoNotCrossPlanes(t *testing.T) {
	h := newHotPathHarness(t)
	h.edgeSetup()
	robot, token := h.onboardWithSession("edge-01")
	satellite := map[string]authFunc{
		"robot basic":     basicAuth(robot.Username, robot.Password),
		"session bearer":  bearerAuth(token),
		"session as user": basicAuth("edge-01", token),
	}

	h.beat(heartbeatRequest("edge-01"), bearerAuth(token))
	h.beat(heartbeatRequest("edge-01"), basicAuth(robot.Username, robot.Password))

	for _, r := range apiRoutes(t) {
		for name, auth := range satellite {
			got := h.do(r.Method, concretePath(r.Path), nil, auth)
			require.Contains(t, []int{http.StatusUnauthorized, http.StatusTooManyRequests}, got.Status, "%s %s with %s: %s", r.Method, r.Path, name, got.Body)
		}
	}
	for r, class := range nonAPIRoutes {
		if class != classSatellite {
			continue
		}
		got := h.do(r.Method, r.Path, heartbeatRequest("edge-01"), h.admin)
		require.Equal(t, http.StatusUnauthorized, got.Status, "%s %s with operator credentials: %s", r.Method, r.Path, got.Body)
	}
}

// The anonymous routes read a small JSON document; a client without
// credentials must not make Ground Control buffer an arbitrarily large one.
func TestRouteSecurity_AnonymousRoutesBoundTheBody(t *testing.T) {
	h := newGCHarness(t)
	big := strings.Repeat("A", 2*anonymousMaxBodyBytes)
	bodies := map[string]any{
		"/login":          LoginRequest{Username: "admin", Password: big},
		"/satellites/ztr": ZTRRequest{Token: big},
	}
	for r, class := range nonAPIRoutes {
		if class != classAnonymous || r.Method != http.MethodPost {
			continue
		}
		body, ok := bodies[r.Path]
		if !ok {
			continue
		}
		got := h.do(r.Method, r.Path, body, nil)
		require.Equal(t, http.StatusRequestEntityTooLarge, got.Status, "%s %s: %s", r.Method, r.Path, got.Body)
	}
	got := h.do(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: harnessAdminPassword}, nil)
	require.Equal(t, http.StatusOK, got.Status, "a normal login still fits: %s", got.Body)
}

// Authenticated /api bodies are bounded too: API_MAX_BODY_BYTES for every
// route, GROUP_SYNC_MAX_BODY_BYTES for the group sync, which carries the
// group's whole artifact list.
func TestRouteSecurity_APIRoutesBoundTheBody(t *testing.T) {
	t.Setenv("API_MAX_BODY_BYTES", "4096")
	t.Setenv("GROUP_SYNC_MAX_BODY_BYTES", "16384")
	h := newGCHarness(t)
	padded := func(prefix string, n int) string {
		return prefix + `"pad":"` + strings.Repeat("A", n) + `"}`
	}
	for _, path := range []string{"/api/configs", "/api/satellites", "/api/users", "/api/policies", "/api/role-bindings", "/api/webhooks"} {
		got := h.do(http.MethodPost, path, padded("{", 8192), h.admin)
		require.Equal(t, http.StatusRequestEntityTooLarge, got.Status, "POST %s: %s", path, got.Body)
	}
	got := h.do(http.MethodPost, "/api/groups/sync", padded(`{"group":"g",`, 32768), h.admin)
	require.Equal(t, http.StatusRequestEntityTooLarge, got.Status, "POST /api/groups/sync: %s", got.Body)

	got = h.do(http.MethodPost, "/api/groups/sync", padded(`{"group":"edge-group","artifacts":[],`, 8192), h.admin)
	require.Equal(t, http.StatusOK, got.Status, "a group sync above API_MAX_BODY_BYTES but within its own limit: %s", got.Body)
}

// The default GROUP_SYNC_MAX_BODY_BYTES holds a group of 10000 artifacts
// with five 128-character tags and a 255-character repository each.
func TestGroupSyncMaxBodyBytesDefaultFitsLargeGroup(t *testing.T) {
	loadGCForTest(t)
	tags := make([]string, 5)
	for i := range tags {
		tags[i] = strings.Repeat(string(rune('a'+i)), 128)
	}
	req := GroupSyncRequest{Group: "large", Registry: "https://harbor.example.com"}
	for i := range 10000 {
		req.Artifacts = append(req.Artifacts, GroupArtifact{
			Repository: fmt.Sprintf("%s/%05d", strings.Repeat("r", 249), i),
			Tag:        tags,
			Digest:     "sha256:" + strings.Repeat("0", 64),
			Type:       "IMAGE",
		})
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	require.Less(t, int64(len(body)), env.GC.Server.GroupSyncMaxBodyBytes, "%d bytes", len(body))
}
