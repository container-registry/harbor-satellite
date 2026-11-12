package server

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
)

// syncRobotPermissions grants each satellite's robot pull access to the
// union of the satellite's groups' projects plus "satellite". One query
// (GetProjectsOfSatellites) covers every satellite and each robot gets one
// Harbor update, so a group sync costs the same statements for any size.
func syncRobotPermissions(ctx context.Context, q *database.Queries, satelliteIDs []int32) error {
	return syncRobotPermissionsWith(ctx, q, harborGroupSync{}, satelliteIDs)
}

// syncRobotPermissionsWith is syncRobotPermissions with the Harbor side
// injected. Harbor replaces a robot's permissions on every update, so each
// robot must get its full project set in one call: a per-group update
// leaves only the last group's projects.
func syncRobotPermissionsWith(ctx context.Context, q *database.Queries, hb groupSyncHarbor, satelliteIDs []int32) error {
	if len(satelliteIDs) == 0 {
		return nil
	}
	robots, err := q.GetProjectsOfSatellites(ctx, satelliteIDs)
	if err != nil {
		return fmt.Errorf("list projects of satellites: %w", err)
	}
	for _, robot := range robots {
		if err := hb.UpdateRobotProjects(ctx, robot.Projects, robot.RobotID); err != nil {
			return fmt.Errorf("update permissions of robot %s: %w", robot.RobotName, err)
		}
	}
	return nil
}

// robotPermissionsError logs err and returns the API error for a failed
// robot permission update.
func robotPermissionsError(err error) *AppError {
	log.Printf("Error updating robot account permissions: %v", err)
	return &AppError{
		Message: "Error: failed to update robot account permissions",
		Code:    http.StatusInternalServerError,
	}
}
