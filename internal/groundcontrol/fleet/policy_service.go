package fleet

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// EventPolicyChanged is log-only: replicas pick up a change by polling
// policy_version, so no handler has anything to do.
const EventPolicyChanged EventKind = "PolicyChanged"

// PolicyChangedPayload records one stored rule or binding change.
type PolicyChangedPayload struct {
	// Name is the rule's subject or the binding's principal.
	Name    string `json:"name"`
	Version int64  `json:"version"`
	// Change is rule_created, rule_deleted, binding_created or
	// binding_deleted.
	Change string `json:"change"`
	ID     int64  `json:"id"`
}

const (
	PolicyRuleCreated    = "rule_created"
	PolicyRuleDeleted    = "rule_deleted"
	PolicyBindingCreated = "binding_created"
	PolicyBindingDeleted = "binding_deleted"
)

// PolicyRule is one stored Casbin policy line. Scope is "*" or
// "<scoped type>:<name>"; Type, Action and Effect come from ResourceTypes,
// Actions and EffectAllow/EffectDeny, or "*" for type and action.
type PolicyRule struct {
	ID                                   int64
	Subject, Scope, Type, Action, Effect string
}

// RoleBinding gives a principal an additional scoped role; the built-in
// roles come from users.role on every request and are never stored.
type RoleBinding struct {
	ID              int64
	Principal, Role string
}

// PolicySnapshot is one consistent read of the stored policy.
type PolicySnapshot struct {
	Version  int64
	Rules    []PolicyRule
	Bindings []RoleBinding
}

// PolicyRepo stores the rules and bindings and the version replicas poll.
// Deletes of a missing ID are ErrNotFound, a duplicate rule or binding is
// ErrAlreadyExists.
type PolicyRepo interface {
	ListRules(ctx context.Context) ([]PolicyRule, error)
	AddRule(ctx context.Context, rule PolicyRule) (PolicyRule, error)
	DeleteRule(ctx context.Context, id int64) error
	ListBindings(ctx context.Context) ([]RoleBinding, error)
	AddBinding(ctx context.Context, binding RoleBinding) (RoleBinding, error)
	DeleteBinding(ctx context.Context, id int64) error
	Version(ctx context.Context) (int64, error)
	BumpVersion(ctx context.Context) (int64, error)
	// Snapshot reads version, rules and bindings in one REPEATABLE READ
	// transaction, version first, so a reload never mixes two versions.
	Snapshot(ctx context.Context) (PolicySnapshot, error)
}

// PolicyService manages the stored rules and bindings (plan item 7.3).
// Each write runs the repo write, BumpVersion and Events.Append of a
// PolicyChanged in one transaction. A write fails with ErrInvalid for:
//   - a subject other than user:, role: or oidc: (a binding's principal:
//     user: or oidc:; its role: role: and not a built-in role),
//   - a scope other than "*" or <scoped type>:<name>,
//   - a type, action or effect outside the vocabulary,
//   - a ',' in any field, and
//   - a scoped rule whose type is not in ScopedTypes() ("*" included),
//
// so a scoped rule can never reach users, SPIRE, ops or the policy API.
type PolicyService struct{}

func (s *PolicyService) Rules(ctx context.Context) ([]PolicyRule, error) {
	return nil, notimpl.Err("7.3 list policy rules")
}

func (s *PolicyService) AddRule(ctx context.Context, rule PolicyRule) (PolicyRule, error) {
	return PolicyRule{}, notimpl.Err("7.3 add policy rule")
}

func (s *PolicyService) DeleteRule(ctx context.Context, id int64) error {
	return notimpl.Err("7.3 delete policy rule")
}

func (s *PolicyService) Bindings(ctx context.Context) ([]RoleBinding, error) {
	return nil, notimpl.Err("7.3 list role bindings")
}

func (s *PolicyService) AddBinding(ctx context.Context, binding RoleBinding) (RoleBinding, error) {
	return RoleBinding{}, notimpl.Err("7.3 add role binding")
}

func (s *PolicyService) DeleteBinding(ctx context.Context, id int64) error {
	return notimpl.Err("7.3 delete role binding")
}
