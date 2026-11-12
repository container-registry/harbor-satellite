package authn

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// SessionStore keeps the satellite_sessions rows; postgres.SessionStore
// implements it. Lookups of an unknown hash or name are fleet.ErrNotFound.
type SessionStore interface {
	// Replace deletes the satellite's sessions and stores hash (ZTR).
	Replace(ctx context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt time.Time) error
	// AddIfNone stores hash only when the satellite has no unexpired
	// session at now; false when it has one, also across replicas.
	AddIfNone(ctx context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt, now time.Time) (bool, error)
	// Renew stores hash as the one successor of session from. A from
	// that another replica deleted meanwhile (re-ZTR, expiry) is
	// fleet.ErrSessionInvalid.
	Renew(ctx context.Context, satellite fleet.SatelliteID, from int64, hash [sha256.Size]byte, expiresAt time.Time) error
	FindByHash(ctx context.Context, hash [sha256.Size]byte) (fleet.SatelliteAuth, error)
	FindByName(ctx context.Context, satelliteName string, now time.Time) (fleet.SatelliteAuth, error)
	DeleteForSatellite(ctx context.Context, satellite fleet.SatelliteID) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// SessionConfig: TTL is a session's lifetime (SATELLITE_SESSION_TTL),
// CacheTTL how long a positive lookup is reused
// (SATELLITE_SESSION_CACHE_TTL), CacheSize the entries kept per cache
// (SATELLITE_SESSION_CACHE_SIZE).
type SessionConfig struct {
	TTL, CacheTTL time.Duration
	CacheSize     int
}

// Sessions issues and checks satellite sessions. One Sessions serves one
// replica; replicas share only the store.
type Sessions struct{}

var _ fleet.SessionIssuer = (*Sessions)(nil)

// NewSessions fails with fleet.ErrInvalid when CacheTTL exceeds MaxSessionCacheTTL.
func NewSessions(store SessionStore, clock fleet.Clock, cfg SessionConfig) (*Sessions, error) {
	return nil, notimpl.Err("4.1 sessions")
}

// Issue replaces the satellite's sessions with a new one. Issue and
// IssueIfNone drop this replica's cached Lookup entry of the satellite.
func (s *Sessions) Issue(ctx context.Context, satellite fleet.SatelliteID) (fleet.SessionGrant, error) {
	return fleet.SessionGrant{}, notimpl.Err("4.1 issue session")
}

func (s *Sessions) IssueIfNone(ctx context.Context, satellite fleet.SatelliteID) (fleet.SessionGrant, bool, error) {
	return fleet.SessionGrant{}, false, notimpl.Err("4.1 issue first session")
}

// Renew stores a successor of auth's session when less than a third of
// the TTL is left; a second renewal of the same session replaces the
// successor instead of adding one.
func (s *Sessions) Renew(ctx context.Context, auth fleet.SatelliteAuth) (fleet.SessionGrant, bool, error) {
	return fleet.SessionGrant{}, false, notimpl.Err("4.1 renew session")
}

func (s *Sessions) Invalidate(ctx context.Context, satellite fleet.SatelliteID) error {
	return notimpl.Err("4.1 invalidate sessions")
}

// Authenticate: positive cache by hash, Method AuthSession; ErrSessionInvalid
// for anything that is not a known session token, ErrSessionExpired, ErrRevoked.
func (s *Sessions) Authenticate(ctx context.Context, token string) (fleet.SatelliteAuth, error) {
	return fleet.SatelliteAuth{}, notimpl.Err("4.1 authenticate session")
}

// Lookup is the cached auth context of an SVID or robot Basic satellite; the
// caller sets Method. An unknown satellite is fleet.ErrNotFound, a revoked
// one fleet.ErrRevoked.
func (s *Sessions) Lookup(ctx context.Context, satelliteName string) (fleet.SatelliteAuth, error) {
	return fleet.SatelliteAuth{}, notimpl.Err("4.1 look up satellite")
}
