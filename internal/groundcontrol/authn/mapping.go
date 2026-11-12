package authn

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// reservedUsername is the bootstrap admin, the break-glass account that
// only a password reaches (plan item 8.2).
const reservedUsername = "admin"

var roleRank = map[string]int{"admin": 1, "system_admin": 2}

// MapPrincipal maps the claims to a principal and a built-in role through
// cfg.RoleMap; the highest mapped role wins. No RoleMap match is
// fleet.ErrForbidden, and nothing is stored for it. The reserved username
// "admin" is fleet.ErrForbidden, so OIDC never reaches the bootstrap admin.
func MapPrincipal(claims Claims, cfg OIDCConfig) (principal fleet.Principal, role string, err error) {
	if claims.Username == "" {
		return fleet.Principal{}, "", fmt.Errorf("%w: the %s claim is empty", fleet.ErrForbidden, cfg.UsernameClaim)
	}
	if claims.Username == reservedUsername {
		return fleet.Principal{}, "", fmt.Errorf("%w: %q is reserved for the bootstrap admin", fleet.ErrForbidden, reservedUsername)
	}
	groups := make([]string, 0, len(claims.Groups))
	for _, g := range claims.Groups {
		groups = append(groups, fleet.SubjectOIDC+g)
		if mapped, ok := cfg.RoleMap[g]; ok && roleRank[mapped] > roleRank[role] {
			role = mapped
		}
	}
	if role == "" {
		return fleet.Principal{}, "", fmt.Errorf("%w: no group of %s maps to a role", fleet.ErrForbidden, claims.Username)
	}
	return fleet.Principal{
		Subject: fleet.SubjectUser + claims.Username,
		Roles:   []string{fleet.SubjectRole + role},
		Groups:  groups,
	}, role, nil
}
