package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/casbin"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func policyRoute(t *testing.T, method, path string) PolicyRoute {
	t.Helper()
	i := slices.IndexFunc(PolicyRoutes, func(e PolicyRoute) bool { return e.Method == method && e.Path == path })
	require.GreaterOrEqual(t, i, 0, "%s %s has no PolicyRoutes entry", method, path)
	return PolicyRoutes[i]
}

// casbinPolicy is the Casbin policy over a stored policy of rules and
// bindings (version 1).
func casbinPolicy(t *testing.T, rules []fleet.PolicyRule, bindings []fleet.RoleBinding) fleet.Policy {
	t.Helper()
	repo := fleettest.NewRepository(nil).Policies
	repo.CurrentVersion, repo.Rules, repo.Bindings = 1, rules, bindings
	p, err := casbin.New(context.Background(), repo, time.Hour)
	require.NoError(t, err)
	return p
}

// The baseline gives every built-in role exactly today's access on every
// /api route: system_admin everything, admin everything requiresSystemAdmin
// does not reserve.
func TestPolicyEquivalence_RouteByRole(t *testing.T) {
	pending.Plan(t, "7.2", "the Casbin policy is a stub")
	p := casbinPolicy(t, nil, nil)

	for _, r := range apiRoutes(t) {
		e := policyRoute(t, r.Method, r.Path)
		for _, role := range []string{roleSystemAdmin, roleAdmin} {
			who := fleet.Principal{Subject: fleet.SubjectUser + "alice", Roles: []string{fleet.SubjectRole + role}}
			want := role == roleSystemAdmin || !requiresSystemAdmin(r.Method, concretePath(r.Path))

			d, err := p.AuthorizeType(context.Background(), who, e.Action, e.Type)

			require.NoError(t, err)
			require.Equal(t, want, d.Allowed, "%s %s as %s (%s %s, rule %q)", r.Method, r.Path, role, e.Type, e.Action, d.Rule)
		}
	}
}

// forbiddenRoutes sends a request to every /api route as an admin and as a
// system admin and returns which ones answered 403.
func forbiddenRoutes(t *testing.T, h *gcHarness, admin authFunc) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, r := range apiRoutes(t) {
		if r.Path == "/api/logout" {
			continue
		}
		path := concretePath(r.Path)
		for role, auth := range map[string]authFunc{roleAdmin: admin, roleSystemAdmin: adminBasicAuth} {
			out[role+" "+r.Method+" "+r.Path] = h.do(r.Method, path, nil, auth).Status == http.StatusForbidden
		}
	}
	return out
}

func TestPolicyMiddleware_CasbinMatchesBuiltin(t *testing.T) {
	pending.Plan(t, "7.2", "the policy route check is a stub")
	operator := basicAuth("operator", "Operat0rPass")
	harness := func(opts ...Option) *gcHarness {
		h := newGCHarness(t, opts...)
		r := h.do(http.MethodPost, "/api/users", CreateUserRequest{Username: "operator", Password: "Operat0rPass"}, h.admin)
		require.Less(t, r.Status, 300, string(r.Body))
		return h
	}
	builtin := forbiddenRoutes(t, harness(), operator)

	t.Setenv("POLICY_ENGINE", env.PolicyEngineCasbin)
	h := harness(WithPolicy(casbinPolicy(t, nil, nil)))
	require.True(t, h.app.policyRouteCheck())

	require.Equal(t, builtin, forbiddenRoutes(t, h, operator))
}

// scopedRouter serves the PolicyRoutes templates behind PolicyMiddleware
// for a fixed principal, answering 200 when the route check lets a request
// through.
func scopedRouter(s *Server, who fleet.Principal) http.Handler {
	router := mux.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(fleet.ContextWithPrincipal(r.Context(), who)))
		})
	})
	router.Use(s.PolicyMiddleware)
	for _, e := range PolicyRoutes {
		router.HandleFunc(e.Path, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }).Methods(e.Method)
	}
	return router
}

// A group admin's scoped rules open the satellite, group and config routes
// and nothing else, even with a scoped wildcard rule stored.
func TestScopedAdmin_CannotReachNonScopedRoutes(t *testing.T) {
	pending.Plan(t, "7.3", "the Casbin policy and the policy route check are stubs")
	p := casbinPolicy(t, []fleet.PolicyRule{
		{ID: 1, Subject: "role:group-admin:A", Scope: "group:A", Type: "group", Action: "*", Effect: "allow"},
		{ID: 2, Subject: "role:group-admin:A", Scope: "group:A", Type: "satellite", Action: "*", Effect: "allow"},
		{ID: 3, Subject: "role:group-admin:A", Scope: "group:A", Type: "*", Action: "*", Effect: "allow"},
	}, []fleet.RoleBinding{{ID: 1, Principal: "user:bob", Role: "role:group-admin:A"}})
	s := &Server{policy: p, policyEngine: env.PolicyEngineCasbin}
	h := scopedRouter(s, fleet.Principal{Subject: "user:bob"})
	status := func(method, path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}

	for _, r := range []routeKey{
		{http.MethodPost, "/api/policies"},
		{http.MethodPost, "/api/role-bindings"},
		{http.MethodPost, "/api/users"},
		{http.MethodGet, "/api/spire/agents"},
		{http.MethodGet, "/api/spire/status"},
		{http.MethodGet, "/api/ops/outbox"},
		{http.MethodPost, "/api/satellites/register"},
	} {
		require.Equal(t, http.StatusForbidden, status(r.Method, r.Path), "%s %s", r.Method, r.Path)
	}
	for _, e := range PolicyRoutes {
		if !slices.Contains(fleet.ScopedTypes(), e.Type) {
			require.Equal(t, http.StatusForbidden, status(e.Method, concretePath(e.Path)), "%s %s", e.Method, e.Path)
		}
	}
	require.Equal(t, http.StatusOK, status(http.MethodPost, "/api/groups/sync"), "the scoped routes stay open; the service checks the group")
	require.Equal(t, http.StatusOK, status(http.MethodGet, "/api/satellites"))
}
