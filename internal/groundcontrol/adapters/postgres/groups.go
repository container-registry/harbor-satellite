package postgres

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type groupRepo struct{}

func (groupRepo) Upsert(ctx context.Context, group fleet.Group) (fleet.Group, error) {
	return fleet.Group{}, notimpl.Err("1.3 postgres upsert group")
}

func (groupRepo) Get(ctx context.Context, id fleet.GroupID) (fleet.Group, error) {
	return fleet.Group{}, notimpl.Err("1.3 postgres get group")
}

func (groupRepo) GetByName(ctx context.Context, name string) (fleet.Group, error) {
	return fleet.Group{}, notimpl.Err("1.3 postgres get group by name")
}

func (groupRepo) List(ctx context.Context) ([]fleet.Group, error) {
	return nil, notimpl.Err("1.3 postgres list groups")
}

func (groupRepo) Delete(ctx context.Context, id fleet.GroupID) error {
	return notimpl.Err("1.3 postgres delete group")
}

func (groupRepo) Members(ctx context.Context, id fleet.GroupID) ([]fleet.SatelliteID, error) {
	return nil, notimpl.Err("1.3 postgres group members")
}

func (groupRepo) AddMember(ctx context.Context, id fleet.GroupID, satellite fleet.SatelliteID) error {
	return notimpl.Err("1.3 postgres add group member")
}

func (groupRepo) RemoveMember(ctx context.Context, id fleet.GroupID, satellite fleet.SatelliteID) error {
	return notimpl.Err("1.3 postgres remove group member")
}
