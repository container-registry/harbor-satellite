package server

import (
	"context"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// syncRobotPermissions grants each satellite's robot pull access to the
// union of the satellite's groups' projects plus "satellite". One query
// (GetProjectsOfSatellites) covers every satellite and each robot gets one
// Harbor update, so a group sync costs the same statements for any size.
func syncRobotPermissions(_ context.Context, _ *database.Queries, satelliteIDs []int32) error {
	return fmt.Errorf("%w: D5 robot permissions for satellites %v", notimpl.ErrNotImplemented, satelliteIDs)
}
