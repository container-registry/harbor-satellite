package authn_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn/authntest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingLifecycle = "ADR-0006 lifecycle is not implemented; satellites are never revoked"

var start = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// clock is a fleet.Clock the test moves.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var cfg = authn.SessionConfig{TTL: 720 * time.Hour, CacheTTL: 30 * time.Second, CacheSize: 100}

func newSessions(t *testing.T, store *authntest.Store, c *clock) *authn.Sessions {
	t.Helper()
	s, err := authn.NewSessions(store, c, cfg)
	require.NoError(t, err)
	return s
}

func onlineAuth(expires time.Time) fleet.SatelliteAuth {
	return fleet.SatelliteAuth{Method: fleet.AuthSession, SessionID: 11, Satellite: 7, SatelliteName: "edge-01", ExpiresAt: expires, State: fleet.StateOnline, HasSession: true}
}

// issued stores a fresh token's auth in store and returns the token.
func issued(t *testing.T, store *authntest.Store, auth fleet.SatelliteAuth) string {
	t.Helper()
	token, hash, err := authn.NewSessionToken()
	require.NoError(t, err)
	store.SetAuth(hash, auth)
	return token
}

func TestSessions_RejectsCacheTTLAboveMax(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	c := &clock{t: start}
	_, err := authn.NewSessions(authntest.NewStore(), c, authn.SessionConfig{TTL: time.Hour, CacheTTL: authn.MaxSessionCacheTTL + time.Second, CacheSize: 1})
	require.ErrorIs(t, err, fleet.ErrInvalid)
	_, err = authn.NewSessions(authntest.NewStore(), c, authn.SessionConfig{TTL: time.Hour, CacheTTL: authn.MaxSessionCacheTTL, CacheSize: 1})
	require.NoError(t, err)
}

func TestSessions_AuthenticateCachesPositive(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	store := authntest.NewStore()
	c := &clock{t: start}
	s := newSessions(t, store, c)
	token := issued(t, store, onlineAuth(start.Add(cfg.TTL)))

	for range 3 {
		auth, err := s.Authenticate(context.Background(), token)
		require.NoError(t, err)
		require.Equal(t, fleet.AuthSession, auth.Method)
		require.Equal(t, fleet.SatelliteID(7), auth.Satellite)
	}
	require.Equal(t, 1, store.Count("FindByHash"), "positive answers are cached")

	_, err := s.Authenticate(context.Background(), "hss_unknown-but-shaped-like-nothing")
	require.ErrorIs(t, err, fleet.ErrSessionInvalid)
	_, err = s.Authenticate(context.Background(), "robot-secret")
	require.ErrorIs(t, err, fleet.ErrSessionInvalid)
	require.Equal(t, 1, store.Count("FindByHash"), "a foreign token never reaches the store")

	c.Advance(cfg.TTL + time.Second)
	_, err = s.Authenticate(context.Background(), token)
	require.ErrorIs(t, err, fleet.ErrSessionExpired)
}

// Two replicas share one store; B keeps accepting a deleted session only
// until its cache entry is older than CacheTTL.
func TestSessions_InvalidationReachesSecondInstanceWithinCacheTTL(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	store := authntest.NewStore()
	c := &clock{t: start}
	a, b := newSessions(t, store, c), newSessions(t, store, c)
	token := issued(t, store, onlineAuth(start.Add(cfg.TTL)))
	_, err := b.Authenticate(context.Background(), token)
	require.NoError(t, err)

	require.NoError(t, a.Invalidate(context.Background(), 7))
	hash, err := authn.HashSessionToken(token)
	require.NoError(t, err)
	store.Forget(hash)
	require.Equal(t, 1, store.Count("DeleteForSatellite"))

	_, err = b.Authenticate(context.Background(), token)
	require.NoError(t, err, "B still holds its cache entry")
	c.Advance(cfg.CacheTTL + time.Second)
	_, err = b.Authenticate(context.Background(), token)
	require.ErrorIs(t, err, fleet.ErrSessionInvalid)
}

func TestSessions_LifecycleRevokeReachesSecondInstanceWithinCacheTTL(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	store := authntest.NewStore()
	c := &clock{t: start}
	b := newSessions(t, store, c)
	auth := onlineAuth(start.Add(cfg.TTL))
	token := issued(t, store, auth)
	_, err := b.Authenticate(context.Background(), token)
	require.NoError(t, err)

	auth.State = fleet.StateRevoked
	hash, err := authn.HashSessionToken(token)
	require.NoError(t, err)
	store.SetAuth(hash, auth)

	_, err = b.Authenticate(context.Background(), token)
	require.NoError(t, err, "B still holds its cache entry")
	c.Advance(cfg.CacheTTL + time.Second)
	_, err = b.Authenticate(context.Background(), token)
	require.ErrorIs(t, err, fleet.ErrRevoked)
}

func TestSessions_RevokedStateIsErrRevoked(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	store := authntest.NewStore()
	c := &clock{t: start}
	s := newSessions(t, store, c)
	auth := onlineAuth(start.Add(cfg.TTL))
	auth.State = fleet.StateRevoked
	token := issued(t, store, auth)
	store.SetByName("edge-01", auth)

	_, err := s.Authenticate(context.Background(), token)
	require.ErrorIs(t, err, fleet.ErrRevoked)
	_, err = s.Lookup(context.Background(), "edge-01")
	require.ErrorIs(t, err, fleet.ErrRevoked)
}

func TestSessions_RenewOnlyInLastThirdOneSuccessor(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	store := authntest.NewStore()
	c := &clock{t: start}
	s := newSessions(t, store, c)
	auth := onlineAuth(start.Add(cfg.TTL))

	_, renewed, err := s.Renew(context.Background(), auth)
	require.NoError(t, err)
	require.False(t, renewed, "two thirds of the TTL left")
	require.Zero(t, store.Count("Renew"))

	c.Advance(cfg.TTL*2/3 + time.Minute)
	grant, renewed, err := s.Renew(context.Background(), auth)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, c.Now().Add(cfg.TTL), grant.ExpiresAt)
	_, _, err = s.Renew(context.Background(), auth)
	require.NoError(t, err)
	calls := store.Calls()
	var froms []int64
	for _, call := range calls {
		if call.Method == "Renew" {
			from, ok := call.Args[1].(int64)
			require.True(t, ok)
			froms = append(froms, from)
		}
	}
	require.Equal(t, []int64{11, 11}, froms, "both renewals name the same predecessor, so the store keeps one successor")
}

func TestSessions_IssueIfNoneDropsLookupCache(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	store := authntest.NewStore()
	c := &clock{t: start}
	s := newSessions(t, store, c)
	robot := fleet.SatelliteAuth{Satellite: 7, SatelliteName: "edge-01", State: fleet.StateOnline}
	store.SetByName("edge-01", robot)
	got, err := s.Lookup(context.Background(), "edge-01")
	require.NoError(t, err)
	require.False(t, got.HasSession)

	store.AddIfNoneOK = true
	grant, ok, err := s.IssueIfNone(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, grant.Token)
	robot.HasSession = true
	store.SetByName("edge-01", robot)

	got, err = s.Lookup(context.Background(), "edge-01")
	require.NoError(t, err)
	require.True(t, got.HasSession, "the cached entry was dropped, so the next beat sees the session")
	require.Equal(t, 2, store.Count("FindByName"))
}
