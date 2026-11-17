package async

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/crypto"
)

// PendingRotation is a staged rotation the satellite has not acked yet.
type PendingRotation struct {
	Satellite    fleet.SatelliteID
	Generation   int64
	Username     string
	SealedSecret []byte
	ExpiresAt    time.Time
}

// ExpiringRobot is a robot due for rotation and the generation its
// successor gets, which robot_rotations holds: a robot name can look like
// a successor's without being one (pre-1.0 robots are named after the
// satellite), so the generation is never read from the name.
type ExpiringRobot struct {
	fleet.Robot
	NextGeneration int64
}

// RotationStore holds robot rotations (plan item 8.4, robot_rotations).
type RotationStore interface {
	// ExpiringBefore lists robots expiring before before whose satellite
	// can take a rotation: a session unexpired at now or a SPIFFE
	// identity, because only those beats deliver the successor.
	ExpiringBefore(ctx context.Context, now, before time.Time, limit int) ([]ExpiringRobot, error)
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
type RobotRotator struct {
	store         RotationStore
	repo          fleet.Repository
	registry      fleet.Registry
	locker        AggregateLocker
	sealer        fleet.Sealer
	clock         fleet.Clock
	robotDuration time.Duration
	rotateBefore  time.Duration
	interval      time.Duration
}

var _ Job = (*RobotRotator)(nil)

// rotationBatch bounds the robots one RunOnce stages or promotes.
const rotationBatch = 100

func NewRobotRotator(store RotationStore, repo fleet.Repository, registry fleet.Registry, locker AggregateLocker, sealer fleet.Sealer, clock fleet.Clock, robotDuration, rotateBefore, interval time.Duration) (*RobotRotator, error) {
	if store == nil || repo == nil || registry == nil || locker == nil || sealer == nil {
		return nil, fmt.Errorf("%w: the robot rotator needs a store, a repository, a registry, a locker and a sealer", fleet.ErrInvalid)
	}
	if robotDuration <= 0 || rotateBefore <= 0 || interval <= 0 {
		return nil, fmt.Errorf("%w: the robot rotator needs positive durations", fleet.ErrInvalid)
	}
	if clock == nil {
		clock = fleet.SystemClock{}
	}
	return &RobotRotator{
		store: store, repo: repo, registry: registry, locker: locker, sealer: sealer, clock: clock,
		robotDuration: robotDuration, rotateBefore: rotateBefore, interval: interval,
	}, nil
}

func (r *RobotRotator) Name() string { return "robot-rotator" }

// Run calls RunOnce every interval until ctx ends.
func (r *RobotRotator) Run(ctx context.Context) error {
	every(ctx, r.interval, func(ctx context.Context) {
		staged, promoted, err := r.RunOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Printf("robot rotator: %v", err)
		case staged+promoted > 0:
			log.Printf("robot rotator: %d staged, %d promoted", staged, promoted)
		}
	})
	return nil
}

func (r *RobotRotator) RunOnce(ctx context.Context) (staged, promoted int, err error) {
	acked, err := r.store.Acked(ctx, rotationBatch)
	if err != nil {
		return 0, 0, fmt.Errorf("list acked rotations: %w", err)
	}
	var errs []error
	for _, rot := range acked {
		ok, lockErr := r.locked(ctx, rot.Satellite, func() error { return r.promote(ctx, rot) })
		if errors.Is(lockErr, fleet.ErrRegistryUnavailable) {
			continue
		}
		if lockErr != nil {
			errs = append(errs, lockErr)
			continue
		}
		if ok {
			promoted++
		}
	}
	now := r.clock.Now()
	expiring, err := r.store.ExpiringBefore(ctx, now, now.Add(r.rotateBefore), rotationBatch)
	if err != nil {
		return 0, promoted, errors.Join(append(errs, fmt.Errorf("list expiring robots: %w", err))...)
	}
	for _, robot := range expiring {
		ok, lockErr := r.locked(ctx, robot.Satellite, func() error { return r.stage(ctx, robot) })
		if errors.Is(lockErr, fleet.ErrRegistryUnavailable) {
			continue
		}
		if lockErr != nil {
			errs = append(errs, lockErr)
			continue
		}
		if ok {
			staged++
		}
	}
	return staged, promoted, errors.Join(errs...)
}

// locked runs fn under the satellite's aggregate lock; ok is false while
// another worker holds it.
func (r *RobotRotator) locked(ctx context.Context, sat fleet.SatelliteID, fn func() error) (bool, error) {
	unlock, ok, err := r.locker.TryLock(ctx, fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(sat)})
	if err != nil || !ok {
		return false, err
	}
	defer unlock()
	return true, fn()
}

func (r *RobotRotator) stage(ctx context.Context, robot ExpiringRobot) error {
	read := r.repo.Autocommit()
	sat, err := read.Satellites().Get(ctx, robot.Satellite)
	if err != nil {
		return fmt.Errorf("satellite %d: %w", robot.Satellite, err)
	}
	grants, err := read.Satellites().RobotGrants(ctx, []fleet.SatelliteID{sat.ID})
	if err != nil {
		return fmt.Errorf("projects of satellite %s: %w", sat.Name, err)
	}
	var projects []string
	for _, g := range grants {
		projects = append(projects, g.Projects...)
	}
	generation := robot.NextGeneration
	name := fleet.SuccessorRobotName(sat.ID, sat.Name, generation)
	creds, err := r.registry.CreateRobot(ctx, fleet.RobotSpec{Name: name, Projects: fleet.RobotProjects(projects), Duration: r.robotDuration})
	if errors.Is(err, fleet.ErrAlreadyExists) {
		// A crash between create and Stage lost the secret: adopt the
		// robot and replace it.
		var found fleet.RobotCredentials
		if found, err = r.registry.FindRobot(ctx, name); err == nil {
			creds, err = r.registry.RotateRobotSecret(ctx, found.HarborID)
		}
	}
	if err != nil {
		return fmt.Errorf("successor robot %s: %w", name, err)
	}
	sealed, err := r.sealer.Seal([]byte(creds.Secret))
	if err != nil {
		return fmt.Errorf("seal successor secret: %w", err)
	}
	hash, err := crypto.HashSecret(creds.Secret)
	if err != nil {
		return fmt.Errorf("hash successor secret: %w", err)
	}
	if creds.Name == "" {
		creds.Name = name
	}
	creds.Secret = ""
	if _, err := r.store.Stage(ctx, fleet.RobotRotation{Satellite: sat.ID, Generation: generation, Successor: creds, Previous: robot.Robot}, sealed, hash); err != nil {
		return fmt.Errorf("stage rotation of %s: %w", sat.Name, err)
	}
	return nil
}

func (r *RobotRotator) promote(ctx context.Context, rot fleet.RobotRotation) error {
	if err := r.registry.DeleteRobot(ctx, rot.Previous.HarborID); err != nil && !errors.Is(err, fleet.ErrNotFound) {
		return fmt.Errorf("delete previous robot %d: %w", rot.Previous.HarborID, err)
	}
	if err := r.store.Promote(ctx, rot); err != nil {
		return fmt.Errorf("promote rotation of satellite %d: %w", rot.Satellite, err)
	}
	return nil
}

// RotationCache refreshes pending rotations every interval in each serve
// process, so a heartbeat finds its rotation without a statement.
type RotationCache struct {
	store    RotationStore
	sealer   fleet.Sealer
	interval time.Duration
	mu       sync.RWMutex
	pending  map[fleet.SatelliteID]fleet.RotatedCredentials
}

var (
	_ fleet.RotationSource = (*RotationCache)(nil)
	_ Job                  = (*RotationCache)(nil)
)

func NewRotationCache(store RotationStore, sealer fleet.Sealer, interval time.Duration) (*RotationCache, error) {
	if store == nil || sealer == nil || interval <= 0 {
		return nil, fmt.Errorf("%w: the rotation cache needs a store, a sealer and a positive interval", fleet.ErrInvalid)
	}
	return &RotationCache{store: store, sealer: sealer, interval: interval, pending: map[fleet.SatelliteID]fleet.RotatedCredentials{}}, nil
}

func (c *RotationCache) Name() string { return "rotation-cache" }

func (c *RotationCache) Run(ctx context.Context) error {
	every(ctx, c.interval, func(ctx context.Context) {
		if err := c.Refresh(ctx); err != nil && ctx.Err() == nil {
			log.Printf("rotation cache: %v", err)
		}
	})
	return nil
}

// Refresh replaces the cache; a rotation whose secret does not open is
// left out, so it is never delivered.
func (c *RotationCache) Refresh(ctx context.Context) error {
	rows, err := c.store.Pending(ctx)
	if err != nil {
		return err
	}
	next := make(map[fleet.SatelliteID]fleet.RotatedCredentials, len(rows))
	var errs []error
	for _, p := range rows {
		secret, err := c.sealer.Open(p.SealedSecret)
		if err != nil {
			errs = append(errs, fmt.Errorf("satellite %d generation %d: %w", p.Satellite, p.Generation, err))
			continue
		}
		next[p.Satellite] = fleet.RotatedCredentials{Username: p.Username, Password: string(secret), ExpiresAt: p.ExpiresAt, Generation: p.Generation}
	}
	c.mu.Lock()
	c.pending = next
	c.mu.Unlock()
	return errors.Join(errs...)
}

func (c *RotationCache) Pending(satellite fleet.SatelliteID) (fleet.RotatedCredentials, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	creds, ok := c.pending[satellite]
	return creds, ok
}
