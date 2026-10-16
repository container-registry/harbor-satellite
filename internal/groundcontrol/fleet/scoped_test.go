package fleet_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingScoped = "authorization is the hard-coded system_admin check; nothing is scoped"

var allow = fleet.Decision{Allowed: true, Rule: "test"}

// g1Admin allows everything on group g1 and on resources whose parents
// include it, and denies the rest.
func g1Admin(_ fleet.Action, r fleet.Resource) fleet.Decision {
	g1 := func(r fleet.Resource) bool { return r.Type == fleet.ResourceGroup && r.ID == "g1" }
	if g1(r) {
		return allow
	}
	for _, p := range r.Parents {
		if g1(p) {
			return allow
		}
	}
	return fleet.Decision{}
}

func (w *world) scoped(t *testing.T, decide func(fleet.Action, fleet.Resource) fleet.Decision) (*fleet.Services, context.Context) {
	t.Helper()
	policy := fleettest.NewPolicy(w.rec)
	policy.Decide = decide
	svc, err := fleet.NewServices(fleet.Deps{
		Repository: w.repo, Registry: w.registry, Identity: w.identity, Policy: policy,
		Clock:    fleettest.Clock{T: now},
		Settings: fleet.Settings{RegistryURL: registryURL, RobotDuration: 30 * 24 * time.Hour, TokenTTL: fleet.DefaultTokenTTL},
	})
	require.NoError(t, err)
	ctx := fleet.ContextWithPrincipal(context.Background(), fleet.Principal{Subject: "user:alice", Roles: []string{"role:group-admin:g1"}})
	return svc, ctx
}

// noWrites asserts that no repository write and no event happened.
func noWrites(t *testing.T, w *world) {
	t.Helper()
	for _, m := range []string{"Satellites.Create", "Satellites.Delete", "Satellites.SetConfig", "Groups.Upsert", "Groups.AddMember", "Tokens.Add", "Events.Append"} {
		require.Zero(t, w.rec.Count(m), "%s after a denial: %v", m, w.rec.Methods())
	}
}

func TestGroupService_ScopedAdminCannotSyncOtherGroup(t *testing.T) {
	pending.Plan(t, "7.3", pendingScoped)
	w := newWorld()
	seedRegistration(w)
	svc, ctx := w.scoped(t, g1Admin)

	_, err := svc.Groups.Sync(ctx, fleet.SyncGroupCmd{Name: "g2"})

	require.ErrorIs(t, err, fleet.ErrForbidden)
	noWrites(t, w)
	_, err = svc.Groups.Sync(ctx, fleet.SyncGroupCmd{Name: "g1"})
	require.NoError(t, err)
}

func TestSatelliteService_RegisterIntoOutOfScopeGroupIsForbidden(t *testing.T) {
	pending.Plan(t, "7.3", pendingScoped)
	w := newWorld()
	seedRegistration(w)
	svc, ctx := w.scoped(t, g1Admin)

	_, err := svc.Satellites.Register(ctx, fleet.RegisterSatelliteCmd{Name: "edge-01", ConfigName: "edge-config", Groups: []string{"g1", "g2"}})

	require.ErrorIs(t, err, fleet.ErrForbidden, "A's admin reaches B through the satellite's parents")
	noWrites(t, w)
	var addMember []string
	for _, c := range w.rec.CallsTo("Authorize") {
		if arg[fleet.Action](t, c, 1) == fleet.ActionAddMember {
			addMember = append(addMember, arg[fleet.Resource](t, c, 2).ID)
		}
	}
	require.Contains(t, addMember, "g2")
}

func TestConfigService_AssignNeedsEveryGroup(t *testing.T) {
	pending.Plan(t, "7.3", pendingScoped)
	w := newWorld()
	seedRegistration(w)
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	w.repo.Groups.NamesOf = map[fleet.SatelliteID][]string{7: {"g1", "g2"}}
	svc, ctx := w.scoped(t, func(a fleet.Action, r fleet.Resource) fleet.Decision {
		if a == fleet.ActionAddMember && r.Type == fleet.ResourceGroup && r.ID == "g2" {
			return fleet.Decision{}
		}
		return allow
	})

	err := svc.Configs.Assign(ctx, fleet.AssignConfigCmd{Satellite: "edge-01", Config: "edge-config"})

	require.ErrorIs(t, err, fleet.ErrForbidden)
	noWrites(t, w)
}

func TestSatelliteService_ListFiltersWithOneParentQuery(t *testing.T) {
	pending.Plan(t, "7.3", pendingScoped)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}, "edge-02": {ID: 8, Name: "edge-02"}}
	w.repo.Groups.NamesOf = map[fleet.SatelliteID][]string{7: {"g1"}, 8: {"g2"}}
	svc, ctx := w.scoped(t, g1Admin)

	got, err := svc.Satellites.List(ctx)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "edge-01", got[0].Name)
	require.Equal(t, 1, w.rec.Count("Groups.GroupsOfSatellites"), "no query per satellite")
}

func TestSatelliteService_OutOfScopeGetIs403(t *testing.T) {
	pending.Plan(t, "7.3", pendingScoped)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}, "edge-02": {ID: 8, Name: "edge-02"}}
	w.repo.Groups.NamesOf = map[fleet.SatelliteID][]string{7: {"g1"}, 8: {"g2"}}
	svc, ctx := w.scoped(t, g1Admin)

	_, err := svc.Satellites.Get(ctx, "edge-02")
	require.ErrorIs(t, err, fleet.ErrForbidden)
	got, err := svc.Satellites.Get(ctx, "edge-01")
	require.NoError(t, err)
	require.Equal(t, fleet.SatelliteID(7), got.ID)
}
