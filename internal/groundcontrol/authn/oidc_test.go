package authn_test

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingOIDC = "operators log in with local passwords only; there is no OIDC"

var oidcCfg = authn.OIDCConfig{
	Enabled:       true,
	IssuerURL:     "https://dex.example",
	UsernameClaim: "preferred_username",
	GroupsClaim:   "groups",
	RoleMap:       map[string]string{"edge-ops": "admin", "platform": "system_admin"},
}

func claims(username string, groups ...string) authn.Claims {
	return authn.Claims{Issuer: "https://dex.example", Subject: "sub-" + username, Username: username, Groups: groups}
}

func TestMapPrincipal_GroupsAndRoleMap(t *testing.T) {
	for name, tc := range map[string]struct {
		groups []string
		role   string
	}{
		"admin group":           {groups: []string{"edge-ops"}, role: "admin"},
		"system_admin group":    {groups: []string{"platform"}, role: "system_admin"},
		"the highest role wins": {groups: []string{"edge-ops", "platform", "other"}, role: "system_admin"},
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "8.1", pendingOIDC)

			principal, role, err := authn.MapPrincipal(claims("alice", tc.groups...), oidcCfg)

			require.NoError(t, err)
			require.Equal(t, tc.role, role)
			require.Equal(t, "user:alice", principal.Subject)
			require.Equal(t, []string{"role:" + tc.role}, principal.Roles)
			groups := make([]string, 0, len(tc.groups))
			for _, g := range tc.groups {
				groups = append(groups, fleet.SubjectOIDC+g)
			}
			require.Equal(t, groups, principal.Groups, "every provider group, mapped or not, for scoped rules")
		})
	}
}

func TestMapPrincipal_UnmappedIsForbidden(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	for _, groups := range [][]string{nil, {"other"}} {
		_, _, err := authn.MapPrincipal(claims("alice", groups...), oidcCfg)
		require.ErrorIs(t, err, fleet.ErrForbidden, "%v", groups)
	}
}

func TestMapPrincipal_ReservedAdminIsForbidden(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	_, _, err := authn.MapPrincipal(claims("admin", "platform"), oidcCfg)
	require.ErrorIs(t, err, fleet.ErrForbidden, "OIDC never reaches the bootstrap admin")
}
