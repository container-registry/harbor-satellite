package fleet

import (
	"context"
	"time"
)

// AuthMethod is how a satellite authenticated (ADR-0016).
type AuthMethod string

const (
	AuthSession AuthMethod = "session"
	AuthSVID    AuthMethod = "svid"
	AuthRobot   AuthMethod = "robot"
)

// SatelliteAuth is what the fleet hot path knows about an authenticated
// satellite without a statement of its own; authn caches it per token and
// per name. SessionID is 0 unless Method is AuthSession.
type SatelliteAuth struct {
	Method          AuthMethod
	SessionID       int64
	Satellite       SatelliteID
	SatelliteName   string
	ExpiresAt       time.Time
	State           LifecycleState
	RobotExpiresAt  time.Time
	InventoryDigest Digest
	// HasSession: the satellite holds an unexpired session (robot and SVID lookups).
	HasSession bool
}

// SessionGrant is a session token as the satellite receives it; Ground
// Control stores only its SHA-256.
type SessionGrant struct {
	Token     string
	ExpiresAt time.Time
}

type SessionIssuer interface {
	// Issue replaces the satellite's sessions with a new one (ZTR).
	Issue(ctx context.Context, satellite SatelliteID) (SessionGrant, error)
	// IssueIfNone issues a session only when the satellite has none, for a
	// robot-Basic satellite onboarded before sessions existed; ok is false
	// when another replica issued one first.
	IssueIfNone(ctx context.Context, satellite SatelliteID) (grant SessionGrant, ok bool, err error)
	// Renew replaces the successor of auth's session (one per session) when
	// less than a third of the TTL is left.
	Renew(ctx context.Context, auth SatelliteAuth) (grant SessionGrant, renewed bool, err error)
	// Invalidate deletes the satellite's sessions and this replica's cache
	// entries; other replicas follow within the cache TTL.
	Invalidate(ctx context.Context, satellite SatelliteID) error
}
