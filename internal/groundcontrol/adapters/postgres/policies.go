package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// policyRepo runs the policy_rules, policy_role_bindings and
// policy_version queries. Snapshot opens its own REPEATABLE READ, read-only
// transaction and reads the version first, so the rules and bindings it
// returns are exactly those of that version even while a write commits.
type policyRepo struct {
	q  *database.Queries
	db *sql.DB
}

func (r policyRepo) ListRules(ctx context.Context) ([]fleet.PolicyRule, error) {
	return listRules(ctx, r.q)
}

func listRules(ctx context.Context, q *database.Queries) ([]fleet.PolicyRule, error) {
	rows, err := q.ListPolicyRules(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.PolicyRule, 0, len(rows))
	for _, row := range rows {
		out = append(out, fleet.PolicyRule{ID: row.ID, Subject: row.Subject, Scope: row.Scope, Type: row.ResourceType, Action: row.Action, Effect: row.Effect})
	}
	return out, nil
}

func (r policyRepo) AddRule(ctx context.Context, rule fleet.PolicyRule) (fleet.PolicyRule, error) {
	row, err := r.q.InsertPolicyRule(ctx, database.InsertPolicyRuleParams{
		Subject: rule.Subject, Scope: rule.Scope, ResourceType: rule.Type, Action: rule.Action, Effect: rule.Effect,
	})
	if err != nil {
		return fleet.PolicyRule{}, mapError(err)
	}
	return fleet.PolicyRule{ID: row.ID, Subject: row.Subject, Scope: row.Scope, Type: row.ResourceType, Action: row.Action, Effect: row.Effect}, nil
}

func (r policyRepo) DeleteRule(ctx context.Context, id int64) error {
	n, err := r.q.DeletePolicyRule(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return fmt.Errorf("policy rule %d: %w", id, fleet.ErrNotFound)
	}
	return nil
}

func (r policyRepo) ListBindings(ctx context.Context) ([]fleet.RoleBinding, error) {
	return listBindings(ctx, r.q)
}

func listBindings(ctx context.Context, q *database.Queries) ([]fleet.RoleBinding, error) {
	rows, err := q.ListRoleBindings(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.RoleBinding, 0, len(rows))
	for _, row := range rows {
		out = append(out, fleet.RoleBinding{ID: row.ID, Principal: row.Principal, Role: row.Role})
	}
	return out, nil
}

func (r policyRepo) AddBinding(ctx context.Context, binding fleet.RoleBinding) (fleet.RoleBinding, error) {
	row, err := r.q.InsertRoleBinding(ctx, database.InsertRoleBindingParams{Principal: binding.Principal, Role: binding.Role})
	if err != nil {
		return fleet.RoleBinding{}, mapError(err)
	}
	return fleet.RoleBinding{ID: row.ID, Principal: row.Principal, Role: row.Role}, nil
}

func (r policyRepo) DeleteBinding(ctx context.Context, id int64) error {
	n, err := r.q.DeleteRoleBinding(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return fmt.Errorf("role binding %d: %w", id, fleet.ErrNotFound)
	}
	return nil
}

func (r policyRepo) Version(ctx context.Context) (int64, error) {
	v, err := r.q.GetPolicyVersion(ctx)
	return v, mapError(err)
}

func (r policyRepo) BumpVersion(ctx context.Context) (int64, error) {
	v, err := r.q.BumpPolicyVersion(ctx)
	return v, mapError(err)
}

func (r policyRepo) Snapshot(ctx context.Context) (snap fleet.PolicySnapshot, err error) {
	if r.db == nil {
		return fleet.PolicySnapshot{}, fmt.Errorf("%w: policy snapshot needs the pool", fleet.ErrInvalid)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return fleet.PolicySnapshot{}, fmt.Errorf("begin policy snapshot: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
	}()
	q := database.New(tx)
	if snap.Version, err = q.GetPolicyVersion(ctx); err != nil {
		return fleet.PolicySnapshot{}, mapError(err)
	}
	if snap.Rules, err = listRules(ctx, q); err != nil {
		return fleet.PolicySnapshot{}, err
	}
	if snap.Bindings, err = listBindings(ctx, q); err != nil {
		return fleet.PolicySnapshot{}, err
	}
	return snap, nil
}
