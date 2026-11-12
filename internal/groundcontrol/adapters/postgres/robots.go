package postgres

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// robotRepo stores Robot.HarborID in robot_accounts.robot_id, a string
// column today.
type robotRepo struct{}

func (robotRepo) Add(ctx context.Context, robot fleet.Robot) (fleet.Robot, error) {
	return fleet.Robot{}, notimpl.Err("1.3 postgres add robot")
}

func (robotRepo) BySatellite(ctx context.Context, satellite fleet.SatelliteID) (fleet.Robot, error) {
	return fleet.Robot{}, notimpl.Err("1.3 postgres robot by satellite")
}

func (robotRepo) ByName(ctx context.Context, name string) (fleet.Robot, error) {
	return fleet.Robot{}, notimpl.Err("1.3 postgres robot by name")
}

func (robotRepo) Update(ctx context.Context, robot fleet.Robot) error {
	return notimpl.Err("1.3 postgres update robot")
}

func (robotRepo) Delete(ctx context.Context, id fleet.RobotID) error {
	return notimpl.Err("1.3 postgres delete robot")
}

// HarborIDs runs ListRobotHarborIDsOfSatellite.
func (robotRepo) HarborIDs(ctx context.Context, satellite fleet.SatelliteID) ([]int64, error) {
	return nil, notimpl.Err("8.4 postgres robot harbor IDs")
}
