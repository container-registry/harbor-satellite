package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.Policy = (*Policy)(nil)

// Policy is a recording fleet.Policy. Authorize records the principal,
// action and resource and returns Decision.
type Policy struct {
	*Recorder
	Decision fleet.Decision
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
	return p.Decision, nil
}

func (p *Policy) Reload(context.Context) error {
	return p.Record("Reload")
}
