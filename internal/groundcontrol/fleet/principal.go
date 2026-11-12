package fleet

import "context"

// Principal is who calls a service: a user, its roles and its OIDC groups
// (plan items 7 and 8). The HTTP layer puts it into the request context.
type Principal struct {
	Subject string
	Roles   []string
	Groups  []string
}

type principalKey struct{}

func ContextWithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal of ctx and whether there is one.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
