package postgres

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// policyRepo runs the policy_rules, policy_role_bindings and
// policy_version queries. Snapshot opens its own REPEATABLE READ, read-only
// transaction and reads the version first, so the rules and bindings it
// returns are exactly those of that version even while a write commits.
type policyRepo struct{}

func (policyRepo) ListRules(ctx context.Context) ([]fleet.PolicyRule, error) {
	return nil, notimpl.Err("7.3 postgres list policy rules")
}

func (policyRepo) AddRule(ctx context.Context, rule fleet.PolicyRule) (fleet.PolicyRule, error) {
	return fleet.PolicyRule{}, notimpl.Err("7.3 postgres add policy rule")
}

func (policyRepo) DeleteRule(ctx context.Context, id int64) error {
	return notimpl.Err("7.3 postgres delete policy rule")
}

func (policyRepo) ListBindings(ctx context.Context) ([]fleet.RoleBinding, error) {
	return nil, notimpl.Err("7.3 postgres list role bindings")
}

func (policyRepo) AddBinding(ctx context.Context, binding fleet.RoleBinding) (fleet.RoleBinding, error) {
	return fleet.RoleBinding{}, notimpl.Err("7.3 postgres add role binding")
}

func (policyRepo) DeleteBinding(ctx context.Context, id int64) error {
	return notimpl.Err("7.3 postgres delete role binding")
}

func (policyRepo) Version(ctx context.Context) (int64, error) {
	return 0, notimpl.Err("7.3 postgres policy version")
}

func (policyRepo) BumpVersion(ctx context.Context) (int64, error) {
	return 0, notimpl.Err("7.3 postgres bump policy version")
}

func (policyRepo) Snapshot(ctx context.Context) (fleet.PolicySnapshot, error) {
	return fleet.PolicySnapshot{}, notimpl.Err("7.3 postgres policy snapshot")
}
