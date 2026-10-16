package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.PolicyRepo = (*PolicyRepo)(nil)

// PolicyRepo is a recording fleet.PolicyRepo. Lists and Snapshot answer
// from Rules, Bindings and CurrentVersion; Add returns its argument with
// NextID, BumpVersion returns CurrentVersion + 1 without storing it.
type PolicyRepo struct {
	rec            *Recorder
	Rules          []fleet.PolicyRule
	Bindings       []fleet.RoleBinding
	CurrentVersion int64
	NextID         int64
}

func (p *PolicyRepo) ListRules(context.Context) ([]fleet.PolicyRule, error) {
	if err := p.rec.Record("Policies.ListRules"); err != nil {
		return nil, err
	}
	return p.Rules, nil
}

func (p *PolicyRepo) AddRule(_ context.Context, rule fleet.PolicyRule) (fleet.PolicyRule, error) {
	if err := p.rec.Record("Policies.AddRule", rule); err != nil {
		return fleet.PolicyRule{}, err
	}
	rule.ID = p.NextID
	return rule, nil
}

func (p *PolicyRepo) DeleteRule(_ context.Context, id int64) error {
	return p.rec.Record("Policies.DeleteRule", id)
}

func (p *PolicyRepo) ListBindings(context.Context) ([]fleet.RoleBinding, error) {
	if err := p.rec.Record("Policies.ListBindings"); err != nil {
		return nil, err
	}
	return p.Bindings, nil
}

func (p *PolicyRepo) AddBinding(_ context.Context, binding fleet.RoleBinding) (fleet.RoleBinding, error) {
	if err := p.rec.Record("Policies.AddBinding", binding); err != nil {
		return fleet.RoleBinding{}, err
	}
	binding.ID = p.NextID
	return binding, nil
}

func (p *PolicyRepo) DeleteBinding(_ context.Context, id int64) error {
	return p.rec.Record("Policies.DeleteBinding", id)
}

func (p *PolicyRepo) Version(context.Context) (int64, error) {
	if err := p.rec.Record("Policies.Version"); err != nil {
		return 0, err
	}
	return p.CurrentVersion, nil
}

func (p *PolicyRepo) BumpVersion(context.Context) (int64, error) {
	if err := p.rec.Record("Policies.BumpVersion"); err != nil {
		return 0, err
	}
	return p.CurrentVersion + 1, nil
}

func (p *PolicyRepo) Snapshot(context.Context) (fleet.PolicySnapshot, error) {
	if err := p.rec.Record("Policies.Snapshot"); err != nil {
		return fleet.PolicySnapshot{}, err
	}
	return fleet.PolicySnapshot{Version: p.CurrentVersion, Rules: p.Rules, Bindings: p.Bindings}, nil
}
