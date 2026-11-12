package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const (
	pendingHotPath   = "the satellite session path answers 501; every beat verifies the robot secret in 8 statements"
	pendingInventory = "every heartbeat rewrites the cached images"
)

// manualClock is moved by the test instead of sleeping.
type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// lazyAuthenticator is handed to NewServer before the harness database
// that its session store reads exists.
type lazyAuthenticator struct{ SatelliteAuthenticator }

// newHotPathHarness is a harness on the session path of plan item 4.1:
// satellites authenticate through authn.Sessions on the postgres session
// store, configured from SATELLITE_SESSION_*, on h.sessionClock. Set GC
// environment variables with t.Setenv before calling it.
func newHotPathHarness(t *testing.T) *gcHarness {
	t.Helper()
	lazy := &lazyAuthenticator{}
	h := newGCHarness(t, WithSatelliteAuthenticator(lazy))
	store, err := postgres.NewSessionStore(h.db.DB)
	require.NoError(t, err)
	srv := env.GC.Server
	clock := &manualClock{t: time.Now()}
	sessions, err := authn.NewSessions(store, clock, authn.SessionConfig{
		TTL: srv.SatelliteSessionTTL, CacheTTL: srv.SatelliteSessionCacheTTL, CacheSize: srv.SatelliteSessionCacheSize,
	})
	require.NoError(t, err)
	lazy.SatelliteAuthenticator = sessions
	h.sessionClock = clock
	return h
}

// splitLimits turns on RATE_LIMIT_SPLIT with the given rates.
func splitLimits(t *testing.T, anonymous, fleetRate string) {
	t.Helper()
	t.Setenv("RATE_LIMIT_SPLIT", "true")
	t.Setenv("RATE_LIMIT_ANONYMOUS", anonymous)
	t.Setenv("RATE_LIMIT_FLEET", fleetRate)
}

// onboardWithSession registers and onboards a satellite and returns its
// robot credentials and its Ground Control session token.
// It runs in config edge-config and group edge-group (edgeSetup).
func (h *gcHarness) onboardWithSession(name string) (RegistryCredentials, string) {
	h.t.Helper()
	cfg := h.ztr(h.registerSatellite(name, "edge-config", "edge-group"))
	require.NotNil(h.t, cfg.Session, "ZTR issues a session")
	return cfg.Auth, cfg.Session.Token
}

// flushHeartbeats writes the buffered heartbeats. Today every beat writes
// in the request, so there is nothing to flush; plan item 4.2 makes this
// run the replica's heartbeat flusher.
func (h *gcHarness) flushHeartbeats() {}

func (h *gcHarness) beat(req SatelliteStatusRequest, auth authFunc) SatelliteSyncResponse {
	h.t.Helper()
	var resp SatelliteSyncResponse
	h.mustDo(http.MethodPost, "/satellites/sync", req, auth, http.StatusOK).decode(h.t, &resp)
	return resp
}

func (h *gcHarness) edgeSetup() {
	h.t.Helper()
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
}

func TestZtr_ReturnsSessionStoredAsHash(t *testing.T) {
	pending.Plan(t, "4.1", pendingHotPath)
	h := newHotPathHarness(t)
	h.edgeSetup()

	_, token := h.onboardWithSession("edge-01")

	require.True(t, strings.HasPrefix(token, authn.SessionTokenPrefix), token)
	var hashes [][]byte
	rows, err := h.db.QueryContext(context.Background(), "SELECT token_hash FROM satellite_sessions")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var hash []byte
		require.NoError(t, rows.Scan(&hash))
		hashes = append(hashes, hash)
	}
	require.NoError(t, rows.Err())
	sum := sha256.Sum256([]byte(token))
	require.Equal(t, [][]byte{sum[:]}, hashes, "only the SHA-256 is stored")
}

func TestSync_AcceptsSessionBearer(t *testing.T) {
	pending.Plan(t, "4.1", pendingHotPath)
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, token := h.onboardWithSession("edge-01")

	h.beat(heartbeatRequest("edge-01"), bearerAuth(token))
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), bearerAuth(authn.SessionTokenPrefix+strings.Repeat("A", 43)), http.StatusUnauthorized)
}

func TestSync_RejectsRobotBasicWhenDisabled(t *testing.T) {
	pending.Plan(t, "4.1", pendingHotPath)
	t.Setenv("SATELLITE_ROBOT_AUTH", "false")
	h := newHotPathHarness(t)
	h.edgeSetup()
	creds, token := h.onboardWithSession("edge-01")

	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, creds.Password), http.StatusUnauthorized)
	h.beat(heartbeatRequest("edge-01"), bearerAuth(token))
}

func TestSync_SessionPathSkipsRobotLookup(t *testing.T) {
	pending.Plan(t, "4.1", pendingHotPath)
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, token := h.onboardWithSession("edge-01")

	used := h.db.MeasureStatements(t, func() { h.beat(heartbeatRequest("edge-01"), bearerAuth(token)) })

	require.Zero(t, used.Calls["GetRobotAccByRobotName"], used.String())
}

// Satellites onboarded before sessions existed get one on their first
// robot-Basic beat, once.
func TestSync_RobotBasicBeatGetsSessionOnce(t *testing.T) {
	pending.Plan(t, "4.1", pendingHotPath)
	h := newHotPathHarness(t)
	h.edgeSetup()
	creds, _ := h.onboardWithSession("edge-01")
	_, err := h.db.ExecContext(context.Background(), "DELETE FROM satellite_sessions")
	require.NoError(t, err)
	robot := basicAuth(creds.Username, creds.Password)

	first := h.beat(heartbeatRequest("edge-01"), robot)
	require.NotNil(t, first.Session)
	h.beat(heartbeatRequest("edge-01"), bearerAuth(first.Session.Token))
	second := h.beat(heartbeatRequest("edge-01"), robot)
	require.Nil(t, second.Session, "the satellite has a session now")
}

func TestSync_MalformedJournalAnswers200WithJournalError(t *testing.T) {
	pending.Plan(t, "6.5", pendingHotPath)
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, token := h.onboardWithSession("edge-01")
	req := heartbeatRequest("edge-01")
	req.Journal = JournalUpload{Epoch: 0, Records: replicationRecords(t, 1)}

	resp := h.beat(req, bearerAuth(token))

	require.NotNil(t, resp.Journal)
	require.NotEmpty(t, resp.Journal.Error)
	require.Zero(t, h.edgeEvents())
}

func TestSync_RequestsInventoryOnDigestChange(t *testing.T) {
	pending.Plan(t, "4.4", pendingInventory)
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, token := h.onboardWithSession("edge-01")
	withList := heartbeatRequest("edge-01")
	withList.InventoryDigest = "sha256:inventory-1"
	digestOnly := withList
	digestOnly.CachedImages = nil

	require.True(t, h.beat(digestOnly, bearerAuth(token)).InventoryRequested, "Ground Control has no inventory for the digest")
	require.False(t, h.beat(withList, bearerAuth(token)).InventoryRequested)
	h.flushHeartbeats()
	require.False(t, h.beat(digestOnly, bearerAuth(token)).InventoryRequested)
	digestOnly.InventoryDigest = "sha256:inventory-2"
	require.True(t, h.beat(digestOnly, bearerAuth(token)).InventoryRequested)
}

// Failed satellite authentications count in D6's per-IP FailureLimiter,
// checked before the Argon2id verify.
func TestSync_FailedSatelliteAuthFeedsFailureLimiter(t *testing.T) {
	pending.Plan(t, "4.6", pendingHotPath)
	t.Setenv("AUTH_FAILURE_LIMIT", "3")
	h := newHotPathHarness(t)
	h.edgeSetup()
	creds, _ := h.onboardWithSession("edge-01")

	for range 3 {
		h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, "wrong"), http.StatusUnauthorized)
	}
	var r response
	got := h.measure(func() {
		r = h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, creds.Password), http.StatusTooManyRequests)
	})
	require.Zero(t, got.statements.Total, "blocked before the robot lookup and its Argon2id")
	var body AppError
	r.decode(t, &body)
	require.Equal(t, http.StatusTooManyRequests, body.Code)
}

func TestBudgetHeartbeat_AtMostOneStatementPerBeat(t *testing.T) {
	pending.Plan(t, "4.2", pendingHotPath)
	splitLimits(t, "100/1m", "100/1s")
	h := newHotPathHarness(t)
	h.edgeSetup()
	tokens := map[string]string{}
	for i := range 10 {
		name := fmt.Sprintf("edge-%02d", i)
		_, tokens[name] = h.onboardWithSession(name)
		h.beat(heartbeatRequest(name), bearerAuth(tokens[name]))
	}
	h.flushHeartbeats()

	got := h.measure(func() {
		for range 10 {
			for name, token := range tokens {
				h.beat(heartbeatRequest(name), bearerAuth(token))
			}
		}
		h.flushHeartbeats()
	})

	requireWithinBudget(t, "100 session heartbeats", got, budget{Statements: 100})
}

func TestBudgetHeartbeat_JournaledBeatCostsAtMostFour(t *testing.T) {
	pending.Plan(t, "6.5", pendingHotPath)
	splitLimits(t, "100/1m", "100/1s")
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, token := h.onboardWithSession("edge-01")
	h.beat(heartbeatRequest("edge-01"), bearerAuth(token))

	measure := func(after int64, n int) int64 {
		t.Helper()
		seqs := make([]int64, n)
		for i := range seqs {
			seqs[i] = after + int64(i) + 1
		}
		req := heartbeatRequest("edge-01")
		req.Journal = JournalUpload{Epoch: journalEpoch, AfterSeq: after, Records: replicationRecords(t, seqs...)}
		got := h.measure(func() { h.beat(req, bearerAuth(token)) })
		requireWithinBudget(t, fmt.Sprintf("journaled heartbeat, %d records", n), got, budget{Statements: 4})
		return got.statements.Total
	}

	require.Equal(t, measure(0, 1), measure(1, 100), "the cost does not grow with the records")
}

func TestBudgetHeartbeat_JournaledRenewingBeatCostsAtMostFive(t *testing.T) {
	pending.Plan(t, "4.1", pendingHotPath)
	splitLimits(t, "100/1m", "100/1s")
	h := newHotPathHarness(t)
	h.edgeSetup()
	_, token := h.onboardWithSession("edge-01")
	h.beat(heartbeatRequest("edge-01"), bearerAuth(token))
	h.sessionClock.Advance(env.GC.Server.SatelliteSessionTTL*2/3 + time.Minute) // into the last third of the session
	req := heartbeatRequest("edge-01")
	req.Journal = JournalUpload{Epoch: journalEpoch, Records: replicationRecords(t, 1)}

	var resp SatelliteSyncResponse
	got := h.measure(func() { resp = h.beat(req, bearerAuth(token)) })

	require.NotNil(t, resp.Session, "renewed")
	requireWithinBudget(t, "journaled renewing heartbeat", got, budget{Statements: 5})
}
