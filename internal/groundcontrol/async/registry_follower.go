package async

import (
	"context"
	"fmt"
	"log"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// GroupFollower is what RegistryFollower needs of the group service.
type GroupFollower interface {
	FollowPush(ctx context.Context, event fleet.RegistryEvent) ([]string, error)
}

// RegistryFollower subscribes to the registry's events and lets groups
// follow pushed tags (plan item 9.5). A failed event is logged and the
// subscription goes on; the next push of the tag heals it.
type RegistryFollower struct {
	source fleet.RegistryEventSource
	groups GroupFollower
}

var _ Job = (*RegistryFollower)(nil)

func NewRegistryFollower(source fleet.RegistryEventSource, groups GroupFollower) (*RegistryFollower, error) {
	if source == nil || groups == nil {
		return nil, fmt.Errorf("%w: the registry follower needs an event source and the group service", fleet.ErrInvalid)
	}
	return &RegistryFollower{source: source, groups: groups}, nil
}

func (f *RegistryFollower) Name() string { return "registry-follower" }

func (f *RegistryFollower) Run(ctx context.Context) error {
	err := f.source.Subscribe(ctx, func(ctx context.Context, e fleet.RegistryEvent) error {
		updated, err := f.groups.FollowPush(ctx, e)
		if err != nil {
			log.Printf("registry follower: %s %s:%s: %v", e.Type, e.Repository, e.Tag, err)
			return err
		}
		if len(updated) > 0 {
			log.Printf("registry follower: %s:%s@%s updated groups %v", e.Repository, e.Tag, e.Digest, updated)
		}
		return nil
	})
	if ctx.Err() != nil {
		return nil
	}
	return err
}
