package async

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// PendingRotation is a staged rotation the satellite has not acked yet.
type PendingRotation struct {
	Satellite    fleet.SatelliteID
	Generation   int64
	Username     string
	SealedSecret []byte
	ExpiresAt    time.Time
}

// RotationStore holds robot rotations (plan item 8.4, robot_rotations).
type RotationStore interface {
	// ExpiringBefore lists robots expiring before before whose satellite
	// can take a rotation: a session unexpired at now or a SPIFFE
	// identity, because only those beats deliver the successor.
	ExpiringBefore(ctx context.Context, now, before time.Time, limit int) ([]fleet.Robot, error)
	Stage(ctx context.Context, rotation fleet.RobotRotation, sealedSecret []byte, secretHash string) (generation int64, err error)
	Pending(ctx context.Context) ([]PendingRotation, error)
	Acked(ctx context.Context, limit int) ([]fleet.RobotRotation, error)
	// Promote appends RobotRotated in its transaction.
	Promote(ctx context.Context, rotation fleet.RobotRotation) error
}

// RobotRotator works per satellite under AggregateLocker. Per satellite:
//   - it creates the successor with SuccessorRobotName and the current
//     RobotProjects. When CreateRobot answers ErrAlreadyExists (a crash
//     between create and Stage), it adopts that exact name with FindRobot +
//     RotateRobotSecret, because the lost secret must be replaced;
//   - it seals the secret and stages the rotation;
//   - for an acked rotation it deletes the old robot (ErrNotFound counts as
//     deleted), then promotes.
//
// ErrRegistryUnavailable skips without failing.
type RobotRotator struct{}

var _ Job = (*RobotRotator)(nil)

func NewRobotRotator(store RotationStore, registry fleet.Registry, locker AggregateLocker, sealer fleet.Sealer, clock fleet.Clock, rotateBefore, interval time.Duration) (*RobotRotator, error) {
	return nil, notimpl.Err("8.4 robot rotator")
}

func (r *RobotRotator) Name() string { return "robot-rotator" }

// Run calls RunOnce every interval until ctx ends.
func (r *RobotRotator) Run(ctx context.Context) error {
	return notimpl.Err("8.4 robot rotator run")
}

func (r *RobotRotator) RunOnce(ctx context.Context) (staged, promoted int, err error) {
	return 0, 0, notimpl.Err("8.4 robot rotator run once")
}

// RotationCache refreshes pending rotations every interval in each serve
// process, so a heartbeat finds its rotation without a statement.
type RotationCache struct{}

var (
	_ fleet.RotationSource = (*RotationCache)(nil)
	_ Job                  = (*RotationCache)(nil)
)

func NewRotationCache(store RotationStore, sealer fleet.Sealer, interval time.Duration) (*RotationCache, error) {
	return nil, notimpl.Err("8.4 rotation cache")
}

func (c *RotationCache) Name() string { return "rotation-cache" }

func (c *RotationCache) Run(ctx context.Context) error {
	return notimpl.Err("8.4 rotation cache run")
}

func (c *RotationCache) Pending(satellite fleet.SatelliteID) (fleet.RotatedCredentials, bool) {
	return fleet.RotatedCredentials{}, false
}
