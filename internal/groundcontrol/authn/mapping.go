package authn

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// MapPrincipal maps the claims to a principal and a built-in role through
// cfg.RoleMap; the highest mapped role wins. No RoleMap match is
// fleet.ErrForbidden, and nothing is stored for it. The reserved username
// "admin" is fleet.ErrForbidden, so OIDC never reaches the bootstrap admin.
func MapPrincipal(claims Claims, cfg OIDCConfig) (principal fleet.Principal, role string, err error) {
	return fleet.Principal{}, "", notimpl.Err("8.1 map OIDC principal")
}
