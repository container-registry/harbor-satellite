package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/stretchr/testify/require"
)

// Tests in this file assert the correct behaviour for defects of the Harbor 3
// plan and fail today; see package pending.

func TestRefreshRotatesRobotSecret(t *testing.T) {
	pending.Plan(t, "D2", "GC does not route the credential refresh the satellite calls")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")

	r := h.mustDo(http.MethodPost, "/satellites/refresh", nil, basicAuth(creds.Username, creds.Password), http.StatusOK)

	var refreshed struct {
		Secret string `json:"secret"`
	}
	r.decode(t, &refreshed)
	require.NotEmpty(t, refreshed.Secret)
	require.NotEqual(t, creds.Password, refreshed.Secret)
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, refreshed.Secret), http.StatusOK)
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, creds.Password), http.StatusUnauthorized)
}

func TestSyncAcceptsSPIFFEIdentityWithoutBasicAuth(t *testing.T) {
	pending.Plan(t, "D3", "SatelliteAuthMiddleware runs before spiffe.AuthMiddleware, so SPIFFE-only satellites get 401 on sync")
	if !spiffeBuild {
		t.Skip("SPIFFE is compiled out")
	}
	h := newGCHarness(t)
	h.createConfig("default")
	h.syncGroup("edge-group", "library/alpine")
	h.onboard("spiffe-01", "default", "edge-group")

	body, err := json.Marshal(heartbeatRequest("spiffe-01"))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	id, err := url.Parse("spiffe://harbor-satellite.local/satellite/region/default/spiffe-01")
	require.NoError(t, err)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{id}}}}

	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestRobotPermissionsAreTheUnionOfAllGroups(t *testing.T) {
	pending.Plan(t, "D5", "each group overwrites the robot permissions, so the last group wins")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("group-a", "alpha/app", "shared/lib")
	h.syncGroup("group-b", "beta/app", "shared/lib")
	want := []string{"alpha", "beta", "satellite", "shared"}

	creds := h.onboard("edge-01", "edge-config", "group-a", "group-b")
	require.Equal(t, want, lastRobotUpdate(t, h.harbor, creds.Username), "PUT payload after registration in two groups")
	require.Equal(t, want, h.harbor.RobotProjects(creds.Username), "robot in Harbor after registration in two groups")

	h.syncGroup("group-a", "alpha/app", "shared/lib")
	require.Equal(t, want, lastRobotUpdate(t, h.harbor, creds.Username), "PUT payload after syncing one of the groups")
	require.Equal(t, want, h.harbor.RobotProjects(creds.Username), "robot in Harbor after syncing one of the groups")
}

// lastRobotUpdate returns the namespaces of the last robot payload GC sent
// to Harbor for this robot.
func lastRobotUpdate(t *testing.T, stub *harbortest.Stub, name string) []string {
	t.Helper()
	calls := stub.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Route != harbortest.RouteUpdateRobot {
			continue
		}
		var robot models.Robot
		require.NoError(t, json.Unmarshal(calls[i].Body, &robot))
		if robot.Name == name {
			return harbortest.PermissionNamespaces(robot.Permissions)
		}
	}
	t.Fatalf("no robot update for %s", name)
	return nil
}

func TestBasicAuthLocksAccountAfterFailedAttempts(t *testing.T) {
	pending.Plan(t, "D6", "/api Basic auth neither records failed attempts nor honours the lockout")
	h := newGCHarness(t)

	for i := range 6 {
		r := h.do(http.MethodGet, "/api/configs", nil, basicAuth("admin", "Wrong-passw0rd"))
		require.Contains(t, []int{http.StatusUnauthorized, http.StatusTooManyRequests}, r.Status, "failed attempt %d", i+1)
	}

	r := h.do(http.MethodGet, "/api/configs", nil, adminBasicAuth)
	require.NotEqual(t, http.StatusOK, r.Status, "the correct password must be rejected while the account is locked")
	r = h.do(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: harnessAdminPassword}, nil)
	require.Equal(t, http.StatusUnauthorized, r.Status, "/login must see the lockout caused by Basic auth")

	// Expire the lockout in the database rather than waiting for it.
	_, err := h.db.ExecContext(context.Background(), "UPDATE login_attempts SET locked_until = NOW() - INTERVAL '1 second' WHERE username = 'admin'")
	require.NoError(t, err)
	h.mustDo(http.MethodGet, "/api/configs", nil, adminBasicAuth, http.StatusOK)
}

func TestZTRTokenCanBeUsedOnlyOnce(t *testing.T) {
	pending.Plan(t, "D7", "the ZTR token is read and deleted without a claim, so concurrent ZTR calls both succeed")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	token := h.registerSatellite("edge-01", "edge-config", "edge-group")

	// Both calls reach Harbor's secret refresh before either deletes the
	// token. A fixed GC rejects the second call earlier; the gate then
	// releases the first one after its timeout.
	h.harbor.Gate(harbortest.RouteRefreshRobot, 2, 3*time.Second)
	reqs := []*http.Request{
		h.newRequest(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil),
		h.newRequest(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil),
	}
	results := make([]response, len(reqs))
	errs := make([]error, len(reqs))
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = send(req)
		}()
	}
	wg.Wait()

	succeeded := 0
	for i := range reqs {
		require.NoError(t, errs[i])
		if results[i].Status == http.StatusOK {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded, "responses: %d %s / %d %s", results[0].Status, results[0].Body, results[1].Status, results[1].Body)
}

func TestCleanupRunsOnceAndReleasesItsLock(t *testing.T) {
	pending.Plan(t, "D8", "the cleanup advisory lock is taken and released through the pool, so the unlock can miss and the lock leaks")
	db := dbtest.New(t)
	ctx := context.Background()
	pool, err := sql.Open("postgres", db.URL)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	pool.SetMaxOpenConns(2)
	s := &Server{db: pool, dbQueries: database.New(pool)}
	logs := captureLog(t)

	blocker, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = blocker.ExecContext(ctx, "LOCK TABLE satellite_status IN EXCLUSIVE MODE")
	require.NoError(t, err)

	first := make(chan struct{})
	go func() {
		defer close(first)
		s.runCleanupWithLock(ctx, defaultRetentionDays)
	}()
	waitFor(t, "first run blocked on satellite_status", func() bool {
		var waiting int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM pg_locks WHERE relation = 'satellite_status'::regclass AND NOT granted").Scan(&waiting))
		return waiting > 0
	})

	s.runCleanupWithLock(ctx, defaultRetentionDays)

	// Exhaust the pool so the first run's next statements are served by a
	// connection other than the one it took the lock on. A cleanup that
	// keeps its lock and its work on one connection does not notice.
	pinned, err := pool.Conn(ctx)
	require.NoError(t, err)
	baseline := pool.Stats().WaitCount
	waiter := make(chan *sql.Conn, 1)
	go func() {
		c, err := pool.Conn(ctx)
		if err != nil {
			close(waiter)
			return
		}
		waiter <- c
	}()
	waitFor(t, "connection request queued", func() bool { return pool.Stats().WaitCount > baseline })
	require.NoError(t, blocker.Commit())
	waitFor(t, "first run waiting for a connection or done", func() bool {
		select {
		case <-first:
			return true
		default:
			return pool.Stats().WaitCount > baseline+1
		}
	})
	require.NoError(t, pinned.Close())
	<-first
	if c, ok := <-waiter; ok {
		require.NoError(t, c.Close())
	}

	require.Equal(t, 1, strings.Count(logs.String(), "Status cleanup completed"), "exactly one of two concurrent runs does the cleanup:\n%s", logs)
	var held int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks
WHERE locktype = 'advisory' AND objid = $1
  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`, cleanupLockID).Scan(&held))
	require.Zero(t, held, "the cleanup advisory lock is still held after both runs returned")
}

func TestCachedImagesAreListedAfterHeartbeat(t *testing.T) {
	pending.Plan(t, "N1", "GetLatestArtifacts compares integer with integer[] (SQLSTATE 42883); the images endpoint always answers 500")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), basicAuth(creds.Username, creds.Password), http.StatusOK)

	r := h.mustDo(http.MethodGet, "/api/satellites/edge-01/images", nil, h.admin, http.StatusOK)

	var images []CachedArtifactResponse
	r.decode(t, &images)
	refs := make([]string, 0, len(images))
	for _, img := range images {
		refs = append(refs, img.Reference)
	}
	require.Equal(t, []string{"library/alpine:3.20", "library/busybox:1.36"}, refs)
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog redirects the standard logger, which GC handlers use, for the
// rest of the test.
func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	prev := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return buf
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
