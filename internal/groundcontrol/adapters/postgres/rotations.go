package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ async.RotationStore = (*RotationStore)(nil)

// RotationStore runs the robot_rotations queries (plan item 8.4). Promote
// runs PromoteRobotRotation with the previous robot's Harbor ID and appends
// RobotRotated in one transaction; 0 rows is no error and appends nothing.
type RotationStore struct {
	db *sql.DB
	q  *database.Queries
}

func NewRotationStore(db *sql.DB) (*RotationStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the rotation store needs a database", fleet.ErrInvalid)
	}
	return &RotationStore{db: db, q: database.New(db)}, nil
}

func (s *RotationStore) ExpiringBefore(ctx context.Context, now, before time.Time, limit int) ([]fleet.Robot, error) {
	rows, err := s.q.ListRobotsExpiringBefore(ctx, database.ListRobotsExpiringBeforeParams{Before: before, Now: now, RowLimit: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.Robot, 0, len(rows))
	for _, r := range rows {
		robot, err := toRobot(database.RobotAccount{
			ID: r.ID, RobotName: r.RobotName, RobotSecretHash: r.RobotSecretHash, RobotID: r.RobotID,
			SatelliteID: r.SatelliteID, RobotExpiry: r.RobotExpiry, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, robot)
	}
	return out, nil
}

func (s *RotationStore) Stage(ctx context.Context, rotation fleet.RobotRotation, sealedSecret []byte, secretHash string) (int64, error) {
	gen, err := s.q.StageRobotRotation(ctx, database.StageRobotRotationParams{
		SatelliteID: int32(rotation.Satellite), Generation: rotation.Generation, RobotName: rotation.Successor.Name,
		RobotHarborID: rotation.Successor.HarborID, SecretCiphertext: sealedSecret, SecretHash: secretHash,
		ExpiresAt: sql.NullTime{Time: rotation.Successor.ExpiresAt, Valid: !rotation.Successor.ExpiresAt.IsZero()},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: satellite %d has a rotation in flight or generation %d does not follow it", fleet.ErrConflict, rotation.Satellite, rotation.Generation)
	}
	return gen, mapError(err)
}

func (s *RotationStore) Pending(ctx context.Context) ([]async.PendingRotation, error) {
	rows, err := s.q.ListPendingRobotRotations(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]async.PendingRotation, 0, len(rows))
	for _, r := range rows {
		out = append(out, async.PendingRotation{
			Satellite: fleet.SatelliteID(r.SatelliteID), Generation: r.Generation, Username: r.RobotName,
			SealedSecret: r.SecretCiphertext, ExpiresAt: nullTime(r.ExpiresAt),
		})
	}
	return out, nil
}

func (s *RotationStore) Acked(ctx context.Context, limit int) ([]fleet.RobotRotation, error) {
	rows, err := s.q.ListAckedRobotRotations(ctx, int32(limit))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.RobotRotation, 0, len(rows))
	for _, r := range rows {
		out = append(out, fleet.RobotRotation{
			Satellite: fleet.SatelliteID(r.SatelliteID), Generation: r.Generation,
			Successor: fleet.RobotCredentials{HarborID: r.RobotHarborID, Name: r.RobotName, ExpiresAt: nullTime(r.ExpiresAt)},
			Previous:  fleet.Robot{ID: fleet.RobotID(r.PreviousID), Satellite: fleet.SatelliteID(r.SatelliteID), Name: r.PreviousName, HarborID: r.PreviousHarborID},
		})
	}
	return out, nil
}

func (s *RotationStore) Promote(ctx context.Context, rotation fleet.RobotRotation) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = tx.Commit()
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
	}()
	q := s.q.WithTx(tx)
	n, err := q.PromoteRobotRotation(ctx, database.PromoteRobotRotationParams{
		Now: time.Now(), SatelliteID: int32(rotation.Satellite), Generation: rotation.Generation, PreviousHarborID: rotation.Previous.HarborID,
	})
	if err != nil || n == 0 {
		return mapError(err)
	}
	sat, err := q.GetSatellite(ctx, int32(rotation.Satellite))
	if err != nil {
		return mapError(err)
	}
	return (&OutboxSink{q: q}).Append(ctx, fleet.Event{
		Kind:      fleet.EventRobotRotated,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(rotation.Satellite)},
		Payload:   fleet.RobotRotatedPayload{Name: sat.Name, Generation: rotation.Generation, RobotHarborIDs: []int64{rotation.Previous.HarborID, rotation.Successor.HarborID}},
	})
}
