package authn

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
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
type Sessions struct {
	store  SessionStore
	clock  fleet.Clock
	cfg    SessionConfig
	byHash *ttlCache[[sha256.Size]byte, fleet.SatelliteAuth]
	byName *ttlCache[string, fleet.SatelliteAuth]
}

var _ fleet.SessionIssuer = (*Sessions)(nil)

// NewSessions fails with fleet.ErrInvalid when CacheTTL exceeds MaxSessionCacheTTL.
func NewSessions(store SessionStore, clock fleet.Clock, cfg SessionConfig) (*Sessions, error) {
	if cfg.CacheTTL > MaxSessionCacheTTL || cfg.CacheTTL < 0 {
		return nil, fmt.Errorf("session cache TTL %s outside 0..%s: %w", cfg.CacheTTL, MaxSessionCacheTTL, fleet.ErrInvalid)
	}
	if cfg.TTL <= 0 {
		return nil, fmt.Errorf("session TTL %s must be positive: %w", cfg.TTL, fleet.ErrInvalid)
	}
	return &Sessions{
		store:  store,
		clock:  clock,
		cfg:    cfg,
		byHash: newTTLCache[[sha256.Size]byte, fleet.SatelliteAuth](cfg.CacheTTL, cfg.CacheSize),
		byName: newTTLCache[string, fleet.SatelliteAuth](cfg.CacheTTL, cfg.CacheSize),
	}, nil
}

// Issue replaces the satellite's sessions with a new one. Issue and
// IssueIfNone drop this replica's cached Lookup entry of the satellite.
func (s *Sessions) Issue(ctx context.Context, satellite fleet.SatelliteID) (fleet.SessionGrant, error) {
	token, hash, err := NewSessionToken()
	if err != nil {
		return fleet.SessionGrant{}, err
	}
	expires := s.clock.Now().Add(s.cfg.TTL)
	if err := s.store.Replace(ctx, satellite, hash, expires); err != nil {
		return fleet.SessionGrant{}, fmt.Errorf("store session of satellite %d: %w", satellite, err)
	}
	s.forget(satellite)
	return fleet.SessionGrant{Token: token, ExpiresAt: expires}, nil
}

func (s *Sessions) IssueIfNone(ctx context.Context, satellite fleet.SatelliteID) (fleet.SessionGrant, bool, error) {
	token, hash, err := NewSessionToken()
	if err != nil {
		return fleet.SessionGrant{}, false, err
	}
	now := s.clock.Now()
	expires := now.Add(s.cfg.TTL)
	ok, err := s.store.AddIfNone(ctx, satellite, hash, expires, now)
	if err != nil {
		return fleet.SessionGrant{}, false, fmt.Errorf("store first session of satellite %d: %w", satellite, err)
	}
	s.forget(satellite)
	if !ok {
		return fleet.SessionGrant{}, false, nil
	}
	return fleet.SessionGrant{Token: token, ExpiresAt: expires}, true, nil
}

// Renew stores a successor of auth's session when less than a third of
// the TTL is left; a second renewal of the same session replaces the
// successor instead of adding one.
func (s *Sessions) Renew(ctx context.Context, auth fleet.SatelliteAuth) (fleet.SessionGrant, bool, error) {
	if auth.Method != fleet.AuthSession || auth.SessionID == 0 {
		return fleet.SessionGrant{}, false, nil
	}
	now := s.clock.Now()
	if auth.ExpiresAt.Sub(now) >= s.cfg.TTL/3 {
		return fleet.SessionGrant{}, false, nil
	}
	token, hash, err := NewSessionToken()
	if err != nil {
		return fleet.SessionGrant{}, false, err
	}
	expires := now.Add(s.cfg.TTL)
	if err := s.store.Renew(ctx, auth.Satellite, auth.SessionID, hash, expires); err != nil {
		return fleet.SessionGrant{}, false, fmt.Errorf("renew session of satellite %d: %w", auth.Satellite, err)
	}
	return fleet.SessionGrant{Token: token, ExpiresAt: expires}, true, nil
}

func (s *Sessions) Invalidate(ctx context.Context, satellite fleet.SatelliteID) error {
	if err := s.store.DeleteForSatellite(ctx, satellite); err != nil {
		return fmt.Errorf("delete sessions of satellite %d: %w", satellite, err)
	}
	s.forget(satellite)
	return nil
}

// Authenticate: positive cache by hash, Method AuthSession; ErrSessionInvalid
// for anything that is not a known session token, ErrSessionExpired, ErrRevoked.
func (s *Sessions) Authenticate(ctx context.Context, token string) (fleet.SatelliteAuth, error) {
	hash, err := HashSessionToken(token)
	if err != nil {
		return fleet.SatelliteAuth{}, err
	}
	now := s.clock.Now()
	auth, ok := s.byHash.get(hash, now)
	if !ok {
		auth, err = s.store.FindByHash(ctx, hash)
		if errors.Is(err, fleet.ErrNotFound) {
			return fleet.SatelliteAuth{}, fleet.ErrSessionInvalid
		}
		if err != nil {
			return fleet.SatelliteAuth{}, fmt.Errorf("look up session: %w", err)
		}
		auth.Method = fleet.AuthSession
		s.byHash.put(hash, auth, now)
	}
	if !now.Before(auth.ExpiresAt) {
		return fleet.SatelliteAuth{}, fleet.ErrSessionExpired
	}
	if auth.State == fleet.StateRevoked {
		return fleet.SatelliteAuth{}, fleet.ErrRevoked
	}
	return auth, nil
}

// Lookup is the cached auth context of an SVID or robot Basic satellite; the
// caller sets Method. An unknown satellite is fleet.ErrNotFound, a revoked
// one fleet.ErrRevoked.
func (s *Sessions) Lookup(ctx context.Context, satelliteName string) (fleet.SatelliteAuth, error) {
	now := s.clock.Now()
	auth, ok := s.byName.get(satelliteName, now)
	if !ok {
		var err error
		auth, err = s.store.FindByName(ctx, satelliteName, now)
		if err != nil {
			return fleet.SatelliteAuth{}, fmt.Errorf("look up satellite %q: %w", satelliteName, err)
		}
		s.byName.put(satelliteName, auth, now)
	}
	if auth.State == fleet.StateRevoked {
		return fleet.SatelliteAuth{}, fleet.ErrRevoked
	}
	return auth, nil
}

// Forget drops this replica's cached auth of the satellite.
func (s *Sessions) Forget(satellite fleet.SatelliteID) { s.forget(satellite) }

func (s *Sessions) forget(satellite fleet.SatelliteID) {
	match := func(a fleet.SatelliteAuth) bool { return a.Satellite == satellite }
	s.byHash.deleteIf(match)
	s.byName.deleteIf(match)
}
