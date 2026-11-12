package casbin

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// The matcher functions take []string lists, never joined strings: user,
// group and OIDC names are not restricted to a charset that keeps a
// separator out, and a name containing one would forge a list entry.

// requestSubjects is r.subs: the principal's subject, roles and OIDC
// groups (user:<n>, role:<r>..., oidc:<g>...). It is built per request, so
// the built-in role of users.role needs no stored binding.
func requestSubjects(principal fleet.Principal) []string {
	return nil
}

// requestObjects is r.objs: <type>:<id> of the resource, then of each of
// its parents.
func requestObjects(resource fleet.Resource) []string {
	return nil
}

// listHas(list, item) is true when item is one entry of list.
func listHas(args ...any) (any, error) {
	return false, notimpl.Err("7.1 casbin listHas")
}

// roleLinker is what anyLinked needs of Casbin's role manager.
type roleLinker interface {
	HasLink(name1, name2 string, domain ...string) (bool, error)
}

// anyLinked returns anyLinked(subs, sub): true when one entry of subs
// equals sub or links has a role link from it to sub (the stored bindings,
// loaded as g lines).
func anyLinked(links roleLinker) func(args ...any) (any, error) {
	return func(...any) (any, error) {
		return false, notimpl.Err("7.1 casbin anyLinked")
	}
}
