package fleet

import (
	"context"
	"fmt"
	"slices"
	"strings"
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
type PolicyService struct{ c *core }

// Built-in roles come from users.role and are never bound.
var builtinRoles = []string{SubjectRole + "admin", SubjectRole + "system_admin"}

func policyResource() Resource { return Resource{Type: ResourcePolicy} }

func (s *PolicyService) Rules(ctx context.Context) ([]PolicyRule, error) {
	if err := s.c.authorize(ctx, ActionList, policyResource()); err != nil {
		return nil, err
	}
	return s.c.read().Policies().ListRules(ctx)
}

func (s *PolicyService) AddRule(ctx context.Context, rule PolicyRule) (PolicyRule, error) {
	if err := validateRule(rule); err != nil {
		return PolicyRule{}, err
	}
	if err := s.c.authorize(ctx, ActionCreate, policyResource()); err != nil {
		return PolicyRule{}, err
	}
	var stored PolicyRule
	err := s.write(ctx, func(tx Tx) (string, int64, string, error) {
		var err error
		stored, err = tx.Policies().AddRule(ctx, rule)
		return rule.Subject, stored.ID, PolicyRuleCreated, err
	})
	return stored, err
}

func (s *PolicyService) DeleteRule(ctx context.Context, id int64) error {
	if err := s.c.authorize(ctx, ActionDelete, policyResource()); err != nil {
		return err
	}
	return s.write(ctx, func(tx Tx) (string, int64, string, error) {
		return "", id, PolicyRuleDeleted, tx.Policies().DeleteRule(ctx, id)
	})
}

func (s *PolicyService) Bindings(ctx context.Context) ([]RoleBinding, error) {
	if err := s.c.authorize(ctx, ActionList, policyResource()); err != nil {
		return nil, err
	}
	return s.c.read().Policies().ListBindings(ctx)
}

func (s *PolicyService) AddBinding(ctx context.Context, binding RoleBinding) (RoleBinding, error) {
	if err := validateBinding(binding); err != nil {
		return RoleBinding{}, err
	}
	if err := s.c.authorize(ctx, ActionCreate, policyResource()); err != nil {
		return RoleBinding{}, err
	}
	var stored RoleBinding
	err := s.write(ctx, func(tx Tx) (string, int64, string, error) {
		var err error
		stored, err = tx.Policies().AddBinding(ctx, binding)
		return binding.Principal, stored.ID, PolicyBindingCreated, err
	})
	return stored, err
}

func (s *PolicyService) DeleteBinding(ctx context.Context, id int64) error {
	if err := s.c.authorize(ctx, ActionDelete, policyResource()); err != nil {
		return err
	}
	return s.write(ctx, func(tx Tx) (string, int64, string, error) {
		return "", id, PolicyBindingDeleted, tx.Policies().DeleteBinding(ctx, id)
	})
}

// write runs change, BumpVersion and the PolicyChanged event in one
// transaction.
func (s *PolicyService) write(ctx context.Context, change func(Tx) (name string, id int64, kind string, err error)) error {
	return s.c.inTx(ctx, func(tx Tx) error {
		name, id, kind, err := change(tx)
		if err != nil {
			return err
		}
		version, err := tx.Policies().BumpVersion(ctx)
		if err != nil {
			return fmt.Errorf("bump policy version: %w", err)
		}
		return tx.Events().Append(ctx, Event{
			Kind:      EventPolicyChanged,
			Aggregate: Aggregate{Type: AggregatePolicy, ID: id},
			Payload:   PolicyChangedPayload{Name: name, Version: version, Change: kind, ID: id},
		})
	})
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func hasKind(s string, kinds ...string) bool {
	for _, k := range kinds {
		if name, ok := strings.CutPrefix(s, k); ok && name != "" {
			return true
		}
	}
	return false
}

func validateRule(r PolicyRule) error {
	for _, f := range []string{r.Subject, r.Scope, r.Type, r.Action, r.Effect} {
		if strings.Contains(f, ",") {
			return invalid("policy fields may not contain ','")
		}
	}
	if !hasKind(r.Subject, SubjectUser, SubjectRole, SubjectOIDC) {
		return invalid("subject %q must be user:, role: or oidc: plus a name", r.Subject)
	}
	if r.Type != "*" && !slices.Contains(ResourceTypes(), ResourceType(r.Type)) {
		return invalid("resource type %q", r.Type)
	}
	if r.Action != "*" && !slices.Contains(Actions(), Action(r.Action)) {
		return invalid("action %q", r.Action)
	}
	if r.Effect != EffectAllow && r.Effect != EffectDeny {
		return invalid("effect %q", r.Effect)
	}
	if r.Scope == "*" {
		return nil
	}
	typ, name, ok := strings.Cut(r.Scope, ":")
	if !ok || name == "" || !slices.Contains(ScopedTypes(), ResourceType(typ)) {
		return invalid("scope %q must be * or <satellite|group|config>:<name>", r.Scope)
	}
	if !slices.Contains(ScopedTypes(), ResourceType(r.Type)) {
		return invalid("a scoped rule must name one of %v, not %q", ScopedTypes(), r.Type)
	}
	return nil
}

func validateBinding(b RoleBinding) error {
	if strings.Contains(b.Principal, ",") || strings.Contains(b.Role, ",") {
		return invalid("binding fields may not contain ','")
	}
	if !hasKind(b.Principal, SubjectUser, SubjectOIDC) {
		return invalid("principal %q must be user: or oidc: plus a name", b.Principal)
	}
	if !hasKind(b.Role, SubjectRole) || slices.Contains(builtinRoles, b.Role) {
		return invalid("role %q must be role: plus a name and not a built-in role", b.Role)
	}
	return nil
}
