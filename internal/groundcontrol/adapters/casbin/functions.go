package casbin

import (
	"fmt"
	"slices"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// The matcher functions take []string lists, never joined strings: user,
// group and OIDC names are not restricted to a charset that keeps a
// separator out, and a name containing one would forge a list entry.

// requestSubjects is r.subs: the principal's subject, roles and OIDC
// groups (user:<n>, role:<r>..., oidc:<g>...). It is built per request, so
// the built-in role of users.role needs no stored binding.
func requestSubjects(principal fleet.Principal) []string {
	out := make([]string, 0, 1+len(principal.Roles)+len(principal.Groups))
	if principal.Subject != "" {
		out = append(out, principal.Subject)
	}
	out = append(out, principal.Roles...)
	return append(out, principal.Groups...)
}

// requestObjects is r.objs: <type>:<id> of the resource, then of each of
// its parents.
func requestObjects(resource fleet.Resource) []string {
	out := make([]string, 0, 1+len(resource.Parents))
	if resource.ID != "" {
		out = append(out, string(resource.Type)+":"+resource.ID)
	}
	for _, p := range resource.Parents {
		out = append(out, string(p.Type)+":"+p.ID)
	}
	return out
}

func listArgs(name string, args []any) ([]string, string, error) {
	if len(args) != 2 {
		return nil, "", fmt.Errorf("%s needs 2 arguments, got %d", name, len(args))
	}
	list, ok := args[0].([]string)
	if !ok {
		return nil, "", fmt.Errorf("%s: first argument is %T, want []string", name, args[0])
	}
	item, ok := args[1].(string)
	if !ok {
		return nil, "", fmt.Errorf("%s: second argument is %T, want string", name, args[1])
	}
	return list, item, nil
}

// listHas(list, item) is true when item is one entry of list.
func listHas(args ...any) (any, error) {
	list, item, err := listArgs("listHas", args)
	if err != nil {
		return false, err
	}
	return slices.Contains(list, item), nil
}

// roleLinker is what anyLinked needs of Casbin's role manager.
type roleLinker interface {
	HasLink(name1, name2 string, domain ...string) (bool, error)
}

// anyLinked returns anyLinked(subs, sub): true when one entry of subs
// equals sub or links has a role link from it to sub (the stored bindings,
// loaded as g lines).
func anyLinked(links roleLinker) func(args ...any) (any, error) {
	return func(args ...any) (any, error) {
		subs, sub, err := listArgs("anyLinked", args)
		if err != nil {
			return false, err
		}
		for _, s := range subs {
			if s == sub {
				return true, nil
			}
			if links == nil {
				continue
			}
			linked, err := links.HasLink(s, sub)
			if err != nil {
				return false, err
			}
			if linked {
				return true, nil
			}
		}
		return false, nil
	}
}
