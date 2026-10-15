package fleet

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type GroupService struct{}

type SyncGroupCmd struct {
	Name      string
	Artifacts []Artifact
}

type MembershipCmd struct {
	Satellite, Group string
}

// Sync stores the group with the distinct projects of its non-deleted
// artifacts, pushes its state and gives every member's robot the union of
// that member's groups. A failure leaves the group unchanged.
func (s *GroupService) Sync(ctx context.Context, cmd SyncGroupCmd) (Group, error) {
	return Group{}, notimpl.Err("1.2 sync group")
}

func (s *GroupService) Delete(ctx context.Context, name string) error {
	return notimpl.Err("1.2 delete group")
}

func (s *GroupService) Get(ctx context.Context, name string) (Group, error) {
	return Group{}, notimpl.Err("1.2 get group")
}

func (s *GroupService) List(ctx context.Context) ([]Group, error) {
	return nil, notimpl.Err("1.2 list groups")
}

func (s *GroupService) Members(ctx context.Context, name string) ([]Satellite, error) {
	return nil, notimpl.Err("1.2 group members")
}

// AddSatellite and RemoveSatellite recompute the robot's projects from all
// of the satellite's groups and republish its state; removing the last
// group publishes a state without groups (D10).
func (s *GroupService) AddSatellite(ctx context.Context, cmd MembershipCmd) error {
	return notimpl.Err("1.2 add satellite to group")
}

func (s *GroupService) RemoveSatellite(ctx context.Context, cmd MembershipCmd) error {
	return notimpl.Err("1.2 remove satellite from group")
}
