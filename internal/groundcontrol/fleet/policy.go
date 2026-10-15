package fleet

import "context"

// ResourceType, Action and Resource name what a principal wants to do;
// plan item 7 adds the constants with the Casbin model.
type (
	ResourceType string
	Action       string
)

type Resource struct {
	Type ResourceType
	ID   string
}

type Decision struct {
	Allowed bool
	// Rule names the policy line that decided, for the audit log.
	Rule string
}

// Policy decides resource-scoped access inside the services; the HTTP
// middleware only checks the route against the role.
type Policy interface {
	Authorize(ctx context.Context, principal Principal, action Action, resource Resource) (Decision, error)
	// Reload picks up changed policies.
	Reload(ctx context.Context) error
}

// AllowAll allows everything: today's behaviour, where any admin may act
// on any resource.
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, Principal, Action, Resource) (Decision, error) {
	return Decision{Allowed: true, Rule: "allow-all"}, nil
}

func (AllowAll) Reload(context.Context) error { return nil }
