package casbin

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

type links map[[2]string]bool

func (l links) HasLink(name1, name2 string, _ ...string) (bool, error) {
	return l[[2]string{name1, name2}], nil
}

func call(t *testing.T, fn func(...any) (any, error), args ...any) bool {
	t.Helper()
	got, err := fn(args...)
	require.NoError(t, err)
	b, ok := got.(bool)
	require.True(t, ok, "%T", got)
	return b
}

func TestRequestFunctions(t *testing.T) {
	pending.Plan(t, "7.1", "the matcher functions are stubs")
	require.Equal(t, []string{"user:bob", "role:admin", "oidc:edge-team"},
		requestSubjects(fleet.Principal{Subject: "user:bob", Roles: []string{"role:admin"}, Groups: []string{"oidc:edge-team"}}))
	require.Equal(t, []string{"satellite:edge-01", "group:A", "group:B"},
		requestObjects(fleet.Resource{Type: fleet.ResourceSatellite, ID: "edge-01", Parents: []fleet.Resource{
			{Type: fleet.ResourceGroup, ID: "A"}, {Type: fleet.ResourceGroup, ID: "B"},
		}}))

	require.True(t, call(t, listHas, []string{"group:A", "group:B"}, "group:B"))
	require.False(t, call(t, listHas, []string{"group:A", "group:B"}, "group:C"))
	require.False(t, call(t, listHas, []string{"group:AB"}, "group:A"), "whole entries only")
	require.False(t, call(t, listHas, []string{"group:X,group:A"}, "group:A"), "a name with a comma is one entry")

	anyLinkedFn := anyLinked(links{{"user:bob", "role:group-admin:A"}: true})
	require.True(t, call(t, anyLinkedFn, []string{"user:bob", "role:admin"}, "role:admin"), "equal")
	require.True(t, call(t, anyLinkedFn, []string{"user:bob", "role:admin"}, "role:group-admin:A"), "linked")
	require.False(t, call(t, anyLinkedFn, []string{"user:alice", "role:admin"}, "role:group-admin:A"))
	require.False(t, call(t, anyLinkedFn, []string{"user:x,role:system_admin"}, "role:system_admin"), "a name with a comma forges no role")
}
