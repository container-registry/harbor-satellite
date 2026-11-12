package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.RegistryEventSource = (*RegistryEventSource)(nil)

// RegistryEventSource delivers Events in order and records what the
// handler returned for each in Errors.
type RegistryEventSource struct {
	Events []fleet.RegistryEvent
	Errors []error
}

func (s *RegistryEventSource) Subscribe(ctx context.Context, handle func(ctx context.Context, event fleet.RegistryEvent) error) error {
	for _, e := range s.Events {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.Errors = append(s.Errors, handle(ctx, e))
	}
	return nil
}
