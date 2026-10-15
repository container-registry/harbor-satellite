package fleet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestPrincipalTravelsInTheContext(t *testing.T) {
	_, ok := fleet.PrincipalFrom(context.Background())
	require.False(t, ok)

	want := fleet.Principal{Subject: "user:alice", Roles: []string{"role:admin"}, Groups: []string{"oidc:edge-team"}}
	got, ok := fleet.PrincipalFrom(fleet.ContextWithPrincipal(context.Background(), want))

	require.True(t, ok)
	require.Equal(t, want, got)
}

func TestAllowAllAllows(t *testing.T) {
	d, err := fleet.AllowAll{}.Authorize(context.Background(), fleet.Principal{}, "delete", fleet.Resource{Type: "satellite", ID: "edge-01"})
	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.NoError(t, fleet.AllowAll{}.Reload(context.Background()))
}

// The services check the principal of the context before they touch
// anything, so plan item 7.3's scoped administration needs no new
// parameters.
func TestServices_AuthorizeThePrincipalOfTheContext(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	seedRegistration(w)
	policy := fleettest.NewPolicy(w.rec)
	svc, err := fleet.NewServices(fleet.Deps{Repository: w.repo, Registry: w.registry, Policy: policy, Clock: fleettest.Clock{T: now}, Settings: fleet.Settings{RegistryURL: registryURL}})
	require.NoError(t, err)
	alice := fleet.Principal{Subject: "user:alice", Roles: []string{"role:group-admin:g1"}}
	ctx := fleet.ContextWithPrincipal(context.Background(), alice)

	_, err = svc.Satellites.Register(ctx, fleet.RegisterSatelliteCmd{Name: "edge-01", ConfigName: "edge-config", Groups: []string{"g1"}})

	require.ErrorIs(t, err, fleet.ErrForbidden)
	authorize := onlyCall(t, w.rec, "Authorize")
	require.Equal(t, alice, arg[fleet.Principal](t, authorize, 0))
	require.Equal(t, []string{"Authorize"}, w.rec.Methods(), "nothing is read or written before the check")

	policy.FailOn("Authorize", errors.New("policy store down"))
	_, err = svc.Satellites.List(ctx)
	require.Error(t, err)
	require.NotErrorIs(t, err, fleet.ErrForbidden, "a policy failure is not a denial")
}
