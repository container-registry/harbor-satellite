package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.Policy = (*Policy)(nil)

// Policy is a recording fleet.Policy. Authorize records the principal,
// action and resource and returns Decide's answer, or Decision when Decide
// is nil; AuthorizeType records the principal, action and type and returns
// TypeDecision.
type Policy struct {
	*Recorder
	Decision     fleet.Decision
	TypeDecision fleet.Decision
	// Decide answers Authorize per resource, for tests where a scoped
	// admin may act on some resources and not on others.
	Decide func(action fleet.Action, resource fleet.Resource) fleet.Decision
}

// NewPolicy denies everything until the test sets Decision; it records
// into rec, or into a new Recorder when rec is nil.
func NewPolicy(rec *Recorder) *Policy {
	return &Policy{Recorder: orNew(rec)}
}

func (p *Policy) Authorize(_ context.Context, principal fleet.Principal, action fleet.Action, resource fleet.Resource) (fleet.Decision, error) {
	if err := p.Record("Authorize", principal, action, resource); err != nil {
		return fleet.Decision{}, err
	}
	if p.Decide != nil {
		return p.Decide(action, resource), nil
	}
	return p.Decision, nil
}

func (p *Policy) AuthorizeType(_ context.Context, principal fleet.Principal, action fleet.Action, typ fleet.ResourceType) (fleet.Decision, error) {
	if err := p.Record("AuthorizeType", principal, action, typ); err != nil {
		return fleet.Decision{}, err
	}
	return p.TypeDecision, nil
}

func (p *Policy) Reload(context.Context) error {
	return p.Record("Reload")
}
