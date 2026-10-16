package fleet

import "context"

// ResourceType, Action and Resource name what a principal wants to do.
type (
	ResourceType string
	Action       string
)

const (
	ResourceSatellite ResourceType = "satellite"
	ResourceGroup     ResourceType = "group"
	ResourceConfig    ResourceType = "config"
	ResourceUser      ResourceType = "user"
	ResourceSPIRE     ResourceType = "spire"
	ResourceSession   ResourceType = "session"
	ResourceOps       ResourceType = "ops"
	ResourcePolicy    ResourceType = "policy"
)

// ResourceTypes lists every ResourceType constant of this package.
func ResourceTypes() []ResourceType {
	return []ResourceType{
		ResourceSatellite, ResourceGroup, ResourceConfig, ResourceUser,
		ResourceSPIRE, ResourceSession, ResourceOps, ResourcePolicy,
	}
}

// ScopedTypes are the only resource types a scoped rule (scope other than
// "*") may name; scoped allows on other types would let a scoped admin reach
// users, SPIRE, ops or the policy API.
func ScopedTypes() []ResourceType {
	return []ResourceType{ResourceSatellite, ResourceGroup, ResourceConfig}
}

const (
	ActionList           Action = "list"
	ActionRead           Action = "read"
	ActionCreate         Action = "create"
	ActionUpdate         Action = "update"
	ActionDelete         Action = "delete"
	ActionSync           Action = "sync"
	ActionAddMember      Action = "add_member"
	ActionRemoveMember   Action = "remove_member"
	ActionAssignConfig   Action = "assign_config"
	ActionRegister       Action = "register"
	ActionRegisterSPIFFE Action = "register_spiffe"
	ActionRevoke         Action = "revoke"
	ActionUnrevoke       Action = "unrevoke"
	ActionChangePassword Action = "change_password"
	ActionSetPassword    Action = "set_password"
	ActionLogout         Action = "logout"
)

// Actions lists every Action constant of this package.
func Actions() []Action {
	return []Action{
		ActionList, ActionRead, ActionCreate, ActionUpdate, ActionDelete,
		ActionSync, ActionAddMember, ActionRemoveMember, ActionAssignConfig,
		ActionRegister, ActionRegisterSPIFFE, ActionRevoke, ActionUnrevoke,
		ActionChangePassword, ActionSetPassword, ActionLogout,
	}
}

// Subjects carry their kind as a prefix, so a user, a role and an OIDC
// group of the same name never match each other's rules.
const (
	SubjectUser = "user:"
	SubjectRole = "role:"
	SubjectOIDC = "oidc:"
)

const (
	EffectAllow = "allow"
	EffectDeny  = "deny"
)

// Resource keeps Type and ID and gains Parents: the scopes it inherits,
// such as a satellite's groups, loaded by the service before it authorizes.
type Resource struct {
	Type    ResourceType
	ID      string
	Parents []Resource
}

type Decision struct {
	Allowed bool
	// Rule names the policy line that decided, for the audit log.
	Rule string
}

// Policy decides access in two layers: the HTTP middleware checks the
// route with AuthorizeType, the services check the resource with
// Authorize after loading its parent scopes.
type Policy interface {
	Authorize(ctx context.Context, principal Principal, action Action, resource Resource) (Decision, error)
	// AuthorizeType is the route check: may the principal do action on some
	// resource of typ? Scoped allows count only for ScopedTypes, scoped
	// denies never.
	AuthorizeType(ctx context.Context, principal Principal, action Action, typ ResourceType) (Decision, error)
	// Reload reloads when policy_version moved and keeps the last good
	// policy when it fails.
	Reload(ctx context.Context) error
}

// AllowAll allows everything: today's behaviour, where any admin may act
// on any resource.
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, Principal, Action, Resource) (Decision, error) {
	return Decision{Allowed: true, Rule: "allow-all"}, nil
}

func (AllowAll) AuthorizeType(context.Context, Principal, Action, ResourceType) (Decision, error) {
	return Decision{Allowed: true, Rule: "allow-all"}, nil
}

func (AllowAll) Reload(context.Context) error { return nil }
