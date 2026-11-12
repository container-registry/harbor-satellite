package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ async.RotationStore = (*RotationStore)(nil)

// RotationStore runs the robot_rotations queries (plan item 8.4). Promote
// runs PromoteRobotRotation with the previous robot's Harbor ID and appends
// RobotRotated in one transaction; 0 rows is no error and appends nothing.
type RotationStore struct{}

func NewRotationStore(db *sql.DB) (*RotationStore, error) {
	return nil, notimpl.Err("8.4 postgres rotation store")
}

func (s *RotationStore) ExpiringBefore(ctx context.Context, now, before time.Time, limit int) ([]fleet.Robot, error) {
	return nil, notimpl.Err("8.4 postgres expiring robots")
}

func (s *RotationStore) Stage(ctx context.Context, rotation fleet.RobotRotation, sealedSecret []byte, secretHash string) (int64, error) {
	return 0, notimpl.Err("8.4 postgres stage rotation")
}

func (s *RotationStore) Pending(ctx context.Context) ([]async.PendingRotation, error) {
	return nil, notimpl.Err("8.4 postgres pending rotations")
}

func (s *RotationStore) Acked(ctx context.Context, limit int) ([]fleet.RobotRotation, error) {
	return nil, notimpl.Err("8.4 postgres acked rotations")
}

func (s *RotationStore) Promote(ctx context.Context, rotation fleet.RobotRotation) error {
	return notimpl.Err("8.4 postgres promote rotation")
}
