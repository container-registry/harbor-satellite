package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// robotRepo stores Robot.HarborID in robot_accounts.robot_id, a string
// column today.
type robotRepo struct{ q *database.Queries }

func parseHarborID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("robot id %q is not a number: %w", s, err)
	}
	return id, nil
}

func toRobot(r database.RobotAccount) (fleet.Robot, error) {
	harborID, err := parseHarborID(r.RobotID)
	if err != nil {
		return fleet.Robot{}, err
	}
	return fleet.Robot{
		ID: fleet.RobotID(r.ID), Satellite: fleet.SatelliteID(r.SatelliteID), Name: r.RobotName,
		HarborID: harborID, SecretHash: r.RobotSecretHash, ExpiresAt: r.RobotExpiry.Time,
	}, nil
}

func expiry(robot fleet.Robot) sql.NullTime {
	return sql.NullTime{Time: robot.ExpiresAt, Valid: !robot.ExpiresAt.IsZero()}
}

func (r robotRepo) Add(ctx context.Context, robot fleet.Robot) (fleet.Robot, error) {
	row, err := r.q.AddRobotAccount(ctx, database.AddRobotAccountParams{
		RobotName: robot.Name, RobotSecretHash: robot.SecretHash, RobotID: strconv.FormatInt(robot.HarborID, 10),
		SatelliteID: int32(robot.Satellite), RobotExpiry: expiry(robot),
	})
	if err != nil {
		return fleet.Robot{}, mapError(err)
	}
	return toRobot(row)
}

func (r robotRepo) BySatellite(ctx context.Context, satellite fleet.SatelliteID) (fleet.Robot, error) {
	row, err := r.q.GetRobotAccBySatelliteID(ctx, int32(satellite))
	if err != nil {
		return fleet.Robot{}, mapError(err)
	}
	return toRobot(row)
}

func (r robotRepo) ByName(ctx context.Context, name string) (fleet.Robot, error) {
	row, err := r.q.GetRobotAccByRobotName(ctx, name)
	if err != nil {
		return fleet.Robot{}, mapError(err)
	}
	return toRobot(row)
}

// Update is fleet.ErrNotFound when the robot is gone, e.g. its satellite
// was deleted after the caller read it.
func (r robotRepo) Update(ctx context.Context, robot fleet.Robot) error {
	n, err := r.q.UpdateRobotAccount(ctx, database.UpdateRobotAccountParams{
		ID: int32(robot.ID), RobotName: robot.Name, RobotSecretHash: robot.SecretHash,
		RobotID: strconv.FormatInt(robot.HarborID, 10), RobotExpiry: expiry(robot),
	})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return fmt.Errorf("robot %d: %w", robot.ID, fleet.ErrNotFound)
	}
	return nil
}

func (r robotRepo) Delete(ctx context.Context, id fleet.RobotID) error {
	return mapError(r.q.DeleteRobotAccount(ctx, int32(id)))
}

// HarborIDs runs ListRobotHarborIDsOfSatellite.
func (r robotRepo) HarborIDs(ctx context.Context, satellite fleet.SatelliteID) ([]int64, error) {
	ids, err := r.q.ListRobotHarborIDsOfSatellite(ctx, int32(satellite))
	return ids, mapError(err)
}
