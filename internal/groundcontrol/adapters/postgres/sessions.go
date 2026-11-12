package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/lib/pq"
)

var (
	_ authn.SessionStore    = (*SessionStore)(nil)
	_ async.ExpiredSessions = (*SessionStore)(nil)
)

// SessionStore runs the satellite_sessions queries on the fleet plane's
// pool: IssueSatelliteSession, IssueFirstSatelliteSession,
// RenewSatelliteSession, GetSatelliteAuthByTokenHash,
// GetSatelliteAuthByName, DeleteSatelliteSessions and
// DeleteExpiredSatelliteSessions. A lookup without a row is
// fleet.ErrNotFound. AddIfNone runs LockSatelliteSessions and
// IssueFirstSatelliteSession in one transaction; Replace and
// DeleteForSatellite run LockSatelliteForSessionWipe first. Renew maps the foreign
// key violation of a predecessor deleted meanwhile (23503) to
// fleet.ErrSessionInvalid.
type SessionStore struct {
	db *sql.DB
	q  *database.Queries
}

func NewSessionStore(db *sql.DB) (*SessionStore, error) {
	if db == nil {
		return nil, fmt.Errorf("session store needs a database: %w", fleet.ErrInvalid)
	}
	return &SessionStore{db: db, q: database.New(db)}, nil
}

// Replace and DeleteForSatellite first wait for renewals in flight: a
// successor committed after their DELETE took its snapshot would survive
// with renewed_from set to NULL.
func (s *SessionStore) Replace(ctx context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt time.Time) error {
	return s.wipe(ctx, satellite, func(q *database.Queries) error {
		return q.IssueSatelliteSession(ctx, database.IssueSatelliteSessionParams{
			SatelliteID: int32(satellite), TokenHash: hash[:], ExpiresAt: expiresAt,
		})
	})
}

func (s *SessionStore) wipe(ctx context.Context, satellite fleet.SatelliteID, run func(*database.Queries) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = mapError(tx.Commit())
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
	}()
	q := s.q.WithTx(tx)
	if err := q.LockSatelliteForSessionWipe(ctx, int32(satellite)); err != nil {
		return mapError(err)
	}
	return mapError(run(q))
}

func (s *SessionStore) AddIfNone(ctx context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt, now time.Time) (added bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err == nil {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
	}()
	q := s.q.WithTx(tx)
	if err = q.LockSatelliteSessions(ctx, database.LockSatelliteSessionsParams{Namespace: advisorylock.SatelliteSessions, SatelliteID: int32(satellite)}); err != nil {
		return false, mapError(err)
	}
	n, err := q.IssueFirstSatelliteSession(ctx, database.IssueFirstSatelliteSessionParams{
		SatelliteID: int32(satellite), TokenHash: hash[:], ExpiresAt: expiresAt, Now: now,
	})
	if err != nil {
		return false, mapError(err)
	}
	if err := tx.Commit(); err != nil {
		return false, mapError(err)
	}
	return n == 1, nil
}

func (s *SessionStore) Renew(ctx context.Context, satellite fleet.SatelliteID, from int64, hash [sha256.Size]byte, expiresAt time.Time) error {
	err := s.q.RenewSatelliteSession(ctx, database.RenewSatelliteSessionParams{
		SatelliteID: int32(satellite), TokenHash: hash[:], ExpiresAt: expiresAt,
		RenewedFrom: sql.NullInt64{Int64: from, Valid: true},
	})
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23503" {
		return fmt.Errorf("%w: predecessor session %d is gone", fleet.ErrSessionInvalid, from)
	}
	return mapError(err)
}

func (s *SessionStore) FindByHash(ctx context.Context, hash [sha256.Size]byte) (fleet.SatelliteAuth, error) {
	row, err := s.q.GetSatelliteAuthByTokenHash(ctx, hash[:])
	if err != nil {
		return fleet.SatelliteAuth{}, mapError(err)
	}
	return fleet.SatelliteAuth{
		Method:          fleet.AuthSession,
		SessionID:       row.SessionID,
		Satellite:       fleet.SatelliteID(row.SatelliteID),
		SatelliteName:   row.SatelliteName,
		ExpiresAt:       row.ExpiresAt,
		State:           fleet.LifecycleState(row.State),
		RobotExpiresAt:  nullTime(row.RobotExpiry),
		InventoryDigest: fleet.Digest(row.InventoryDigest),
		HasSession:      true,
	}, nil
}

func (s *SessionStore) FindByName(ctx context.Context, satelliteName string, now time.Time) (fleet.SatelliteAuth, error) {
	row, err := s.q.GetSatelliteAuthByName(ctx, database.GetSatelliteAuthByNameParams{Now: now, Name: satelliteName})
	if err != nil {
		return fleet.SatelliteAuth{}, mapError(err)
	}
	return fleet.SatelliteAuth{
		Satellite:       fleet.SatelliteID(row.SatelliteID),
		SatelliteName:   row.SatelliteName,
		State:           fleet.LifecycleState(row.State),
		RobotExpiresAt:  nullTime(row.RobotExpiry),
		InventoryDigest: fleet.Digest(row.InventoryDigest),
		HasSession:      row.HasSession,
	}, nil
}

func (s *SessionStore) DeleteForSatellite(ctx context.Context, satellite fleet.SatelliteID) error {
	return s.wipe(ctx, satellite, func(q *database.Queries) error {
		return q.DeleteSatelliteSessions(ctx, int32(satellite))
	})
}

func (s *SessionStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.q.DeleteExpiredSatelliteSessions(ctx, now)
	return n, mapError(err)
}

func nullTime(t sql.NullTime) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}
