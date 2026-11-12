package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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
// IssueFirstSatelliteSession in one transaction. Renew maps the foreign
// key violation of a predecessor deleted meanwhile (23503) to
// fleet.ErrSessionInvalid.
type SessionStore struct{}

func NewSessionStore(db *sql.DB) (*SessionStore, error) {
	return nil, notimpl.Err("4.1 postgres session store")
}

func (s *SessionStore) Replace(ctx context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt time.Time) error {
	return notimpl.Err("4.1 postgres replace sessions")
}

func (s *SessionStore) AddIfNone(ctx context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt, now time.Time) (bool, error) {
	return false, notimpl.Err("4.1 postgres add first session")
}

func (s *SessionStore) Renew(ctx context.Context, satellite fleet.SatelliteID, from int64, hash [sha256.Size]byte, expiresAt time.Time) error {
	return notimpl.Err("4.1 postgres renew session")
}

func (s *SessionStore) FindByHash(ctx context.Context, hash [sha256.Size]byte) (fleet.SatelliteAuth, error) {
	return fleet.SatelliteAuth{}, notimpl.Err("4.1 postgres find session")
}

func (s *SessionStore) FindByName(ctx context.Context, satelliteName string, now time.Time) (fleet.SatelliteAuth, error) {
	return fleet.SatelliteAuth{}, notimpl.Err("4.1 postgres find satellite auth")
}

func (s *SessionStore) DeleteForSatellite(ctx context.Context, satellite fleet.SatelliteID) error {
	return notimpl.Err("4.1 postgres delete sessions")
}

func (s *SessionStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	return 0, notimpl.Err("4.1 postgres delete expired sessions")
}
