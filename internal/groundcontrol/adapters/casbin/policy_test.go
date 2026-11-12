package casbin_test

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	casbinlib "github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/casbin"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func never(...any) (any, error) { return false, nil }

func baselineLines(t *testing.T) [][]string {
	t.Helper()
	var lines [][]string
	for line := range strings.Lines(casbin.Baseline) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		lines = append(lines, fields)
	}
	require.NotEmpty(t, lines)
	return lines
}

// The models and the route matcher compile with Casbin and the baseline
// loads into the GC model with the fleet vocabulary only.
func TestModelsParse(t *testing.T) {
	m, err := model.NewModelFromString(casbin.Model)
	require.NoError(t, err)
	for _, fields := range baselineLines(t) {
		require.NoError(t, persist.LoadPolicyArray(fields, m), "%v", fields)
		require.Equal(t, "p", fields[0], "the baseline has no bindings: %v", fields)
		require.Len(t, fields, 6, "%v", fields)
		require.True(t, strings.HasPrefix(fields[1], fleet.SubjectRole), "%v", fields)
		require.Equal(t, "*", fields[2], "the baseline is unscoped: %v", fields)
		require.True(t, fields[3] == "*" || slices.Contains(fleet.ResourceTypes(), fleet.ResourceType(fields[3])), "%v", fields)
		require.True(t, fields[4] == "*" || slices.Contains(fleet.Actions(), fleet.Action(fields[4])), "%v", fields)
		require.Contains(t, []string{fleet.EffectAllow, fleet.EffectDeny}, fields[5], "%v", fields)
	}
	e, err := casbinlib.NewEnforcer(m)
	require.NoError(t, err)
	e.AddFunction("anyLinked", never)
	e.AddFunction("listHas", never)
	allowed, err := e.Enforce([]string{"user:alice", "role:admin"}, []string{"satellite:edge-01", "group:A"}, "satellite", "read")
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = e.EnforceWithMatcher(casbin.RouteMatcher, []string{"user:alice", "role:admin"}, []string{}, "satellite", "read")
	require.NoError(t, err)
	require.False(t, allowed)

	edge, err := model.NewModelFromString(policybundle.Model)
	require.NoError(t, err)
	enforcer, err := casbinlib.NewEnforcer(edge)
	require.NoError(t, err)
	for _, line := range [][]string{{"*", "*", "*", "*", "allow"}, {"*", "library/blocked", "*", "*", "deny"}, {"*", "library/alpine", "3.1*", "pull_manifest", "deny"}} {
		_, err := enforcer.AddPolicy(line)
		require.NoError(t, err)
	}
	for _, tc := range []struct {
		repo, ref, act string
		want           bool
	}{
		{"library/alpine", "latest", "pull_manifest", true},
		{"library/blocked", "latest", "pull_manifest", false},
		{"library/alpine", "3.19", "pull_manifest", false},
		{"library/alpine", "*", "pull_blob", true},
	} {
		got, err := enforcer.Enforce("", tc.repo, tc.ref, tc.act)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "%+v", tc)
	}

	fresh, err := model.NewModelFromString(policybundle.Model)
	require.NoError(t, err)
	tagOnly, err := casbinlib.NewEnforcer(fresh)
	require.NoError(t, err)
	_, err = tagOnly.AddPolicy([]string{"*", "library/app", "v1*", "*", "allow"})
	require.NoError(t, err)
	for _, tc := range []struct {
		ref, act string
		want     bool
	}{
		{"v1.2", "pull_manifest", true},
		{"v2.0", "pull_manifest", false},
		{"*", "pull_blob", true},
	} {
		got, err := tagOnly.Enforce("", "library/app", tc.ref, tc.act)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "tag-only allow: %+v", tc)
	}
}

// RouteMatcher names the scoped types itself; they must stay
// fleet.ScopedTypes.
func TestRouteMatcher_NamesTheScopedTypes(t *testing.T) {
	var got []fleet.ResourceType
	for _, m := range regexp.MustCompile(`p\.typ == "([a-z]+)"`).FindAllStringSubmatch(casbin.RouteMatcher, -1) {
		got = append(got, fleet.ResourceType(m[1]))
	}
	require.ElementsMatch(t, fleet.ScopedTypes(), got)
}

func newPolicy(t *testing.T, repo *fleettest.PolicyRepo) *casbin.Policy {
	t.Helper()
	p, err := casbin.New(context.Background(), repo, time.Hour)
	require.NoError(t, err)
	return p
}

func groupAdminOfA() (rules []fleet.PolicyRule, bindings []fleet.RoleBinding) {
	return []fleet.PolicyRule{
			{ID: 1, Subject: "role:group-admin:A", Scope: "group:A", Type: "group", Action: "*", Effect: "allow"},
			{ID: 2, Subject: "role:group-admin:A", Scope: "group:A", Type: "satellite", Action: "*", Effect: "allow"},
		}, []fleet.RoleBinding{
			{ID: 1, Principal: "user:bob", Role: "role:group-admin:A"},
		}
}

func seededRepo() (*fleettest.Repository, *fleettest.PolicyRepo) {
	repo := fleettest.NewRepository(nil)
	repo.Policies.CurrentVersion = 1
	repo.Policies.Rules, repo.Policies.Bindings = groupAdminOfA()
	return repo, repo.Policies
}

func authorize(t *testing.T, p fleet.Policy, who fleet.Principal, action fleet.Action, resource fleet.Resource) bool {
	t.Helper()
	d, err := p.Authorize(context.Background(), who, action, resource)
	require.NoError(t, err)
	if d.Allowed {
		require.NotEmpty(t, d.Rule, "an allow names its line")
	}
	return d.Allowed
}

func authorizeType(t *testing.T, p fleet.Policy, who fleet.Principal, action fleet.Action, typ fleet.ResourceType) bool {
	t.Helper()
	d, err := p.AuthorizeType(context.Background(), who, action, typ)
	require.NoError(t, err)
	return d.Allowed
}

var bob = fleet.Principal{Subject: "user:bob"}

func satelliteIn(name string, groups ...string) fleet.Resource {
	r := fleet.Resource{Type: fleet.ResourceSatellite, ID: name}
	for _, g := range groups {
		r.Parents = append(r.Parents, fleet.Resource{Type: fleet.ResourceGroup, ID: g})
	}
	return r
}

func TestCasbinPolicy_GroupAdminScopedToGroup(t *testing.T) {
	pending.Plan(t, "7.3", "the Casbin policy is a stub")
	_, repo := seededRepo()
	p := newPolicy(t, repo)

	require.True(t, authorize(t, p, bob, fleet.ActionSync, fleet.Resource{Type: fleet.ResourceGroup, ID: "A"}))
	require.False(t, authorize(t, p, bob, fleet.ActionSync, fleet.Resource{Type: fleet.ResourceGroup, ID: "B"}))
	require.True(t, authorize(t, p, bob, fleet.ActionRead, satelliteIn("edge-01", "A")), "a satellite in A")
	require.True(t, authorize(t, p, bob, fleet.ActionRead, satelliteIn("edge-03", "B", "A")), "a satellite in A among others")
	require.False(t, authorize(t, p, bob, fleet.ActionRead, satelliteIn("edge-02", "B")))
	require.False(t, authorize(t, p, bob, fleet.ActionRead, satelliteIn("edge-04")), "a groupless satellite")
	require.False(t, authorize(t, p, bob, fleet.ActionDelete, fleet.Resource{Type: fleet.ResourceConfig, ID: "edge-config"}))

	require.True(t, authorizeType(t, p, bob, fleet.ActionSync, fleet.ResourceGroup), "the route check lets a scoped admin through")
	require.True(t, authorizeType(t, p, bob, fleet.ActionList, fleet.ResourceSatellite))
	require.False(t, authorizeType(t, p, bob, fleet.ActionCreate, fleet.ResourceUser))
	require.False(t, authorizeType(t, p, bob, fleet.ActionList, fleet.ResourceConfig), "no rule on configs")
}

func TestCasbinPolicy_DenyOverridesAcrossSubjects(t *testing.T) {
	pending.Plan(t, "7.3", "the Casbin policy is a stub")
	_, repo := seededRepo()
	repo.Rules = append(repo.Rules,
		fleet.PolicyRule{ID: 3, Subject: "oidc:contractors", Scope: "*", Type: "satellite", Action: "delete", Effect: "deny"},
		fleet.PolicyRule{ID: 4, Subject: "role:no-sync", Scope: "group:A", Type: "group", Action: "sync", Effect: "deny"},
	)
	repo.Bindings = append(repo.Bindings, fleet.RoleBinding{ID: 2, Principal: "user:bob", Role: "role:no-sync"})
	p := newPolicy(t, repo)
	alice := fleet.Principal{Subject: "user:alice", Roles: []string{"role:admin"}, Groups: []string{"oidc:contractors"}}

	require.False(t, authorize(t, p, alice, fleet.ActionDelete, satelliteIn("edge-01", "A")), "the group's deny beats the role's allow")
	require.True(t, authorize(t, p, alice, fleet.ActionRead, satelliteIn("edge-01", "A")))
	require.False(t, authorize(t, p, bob, fleet.ActionSync, fleet.Resource{Type: fleet.ResourceGroup, ID: "A"}), "a bound role's deny beats another bound role's allow")
	require.True(t, authorize(t, p, bob, fleet.ActionRead, fleet.Resource{Type: fleet.ResourceGroup, ID: "A"}))
}

// PolicyService rejects these rules, but the route check must stay closed
// even if one is stored: a scoped rule never opens a route outside
// satellite, group and config, and a scoped deny never closes one.
func TestCasbinPolicy_ScopedWildcardTypeIgnoredByRouteMatcher(t *testing.T) {
	pending.Plan(t, "7.3", "the Casbin policy is a stub")
	_, repo := seededRepo()
	repo.Rules = append(repo.Rules,
		fleet.PolicyRule{ID: 3, Subject: "user:mallory", Scope: "group:A", Type: "*", Action: "*", Effect: "allow"},
		fleet.PolicyRule{ID: 4, Subject: "user:mallory", Scope: "group:A", Type: "user", Action: "create", Effect: "allow"},
		fleet.PolicyRule{ID: 5, Subject: "user:carol", Scope: "group:A", Type: "group", Action: "sync", Effect: "deny"},
	)
	p := newPolicy(t, repo)
	mallory := fleet.Principal{Subject: "user:mallory"}
	carol := fleet.Principal{Subject: "user:carol", Roles: []string{"role:admin"}}

	for _, typ := range []fleet.ResourceType{fleet.ResourceUser, fleet.ResourceSPIRE, fleet.ResourceOps, fleet.ResourcePolicy, fleet.ResourceGroup} {
		require.False(t, authorizeType(t, p, mallory, fleet.ActionCreate, typ), "%s", typ)
	}
	require.True(t, authorizeType(t, p, carol, fleet.ActionSync, fleet.ResourceGroup), "a scoped deny does not close the route")
	require.False(t, authorize(t, p, carol, fleet.ActionSync, fleet.Resource{Type: fleet.ResourceGroup, ID: "A"}), "it denies in the service")
	require.True(t, authorize(t, p, carol, fleet.ActionSync, fleet.Resource{Type: fleet.ResourceGroup, ID: "B"}))
}

func TestCasbinPolicy_ReloadsOnVersionChange(t *testing.T) {
	pending.Plan(t, "7.3", "the Casbin policy is a stub")
	repo := fleettest.NewRepository(nil)
	repo.Policies.CurrentVersion = 1
	p := newPolicy(t, repo.Policies)
	groupA := fleet.Resource{Type: fleet.ResourceGroup, ID: "A"}
	require.False(t, authorize(t, p, bob, fleet.ActionSync, groupA))

	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, 1, repo.Count("Policies.Snapshot"), "an unchanged version is not reloaded")

	repo.Policies.CurrentVersion = 2
	repo.Policies.Rules, repo.Policies.Bindings = groupAdminOfA()
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, 2, repo.Count("Policies.Snapshot"))
	require.True(t, authorize(t, p, bob, fleet.ActionSync, groupA))
}

func TestCasbinPolicy_FailedReloadKeepsLastGood(t *testing.T) {
	pending.Plan(t, "7.3", "the Casbin policy is a stub")
	ctx := context.Background()
	repo, policies := seededRepo()
	down := errors.New("database down")
	repo.FailOn("Policies.Snapshot", down)
	_, err := casbin.New(ctx, policies, time.Hour)
	require.ErrorIs(t, err, down, "serve fails closed without a policy")
	repo.ClearFaults()
	p := newPolicy(t, policies)
	groupA := fleet.Resource{Type: fleet.ResourceGroup, ID: "A"}

	policies.CurrentVersion = 2
	repo.FailOn("Policies.Snapshot", down)
	require.ErrorIs(t, p.Reload(ctx), down)
	require.True(t, authorize(t, p, bob, fleet.ActionSync, groupA), "the last good policy stays")

	repo.ClearFaults()
	policies.CurrentVersion = 3
	policies.Rules = []fleet.PolicyRule{{ID: 9, Subject: "user:bob", Scope: "*", Type: "group", Action: "sync", Effect: "maybe"}}
	require.Error(t, p.Reload(ctx), "a snapshot that does not load")
	require.True(t, authorize(t, p, bob, fleet.ActionSync, groupA), "the last good policy stays")
}
