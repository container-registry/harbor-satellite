package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/harbor2"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const placeholderSecret = "spiffe-auto-registered-placeholder-secret"

func TestListOutboxEvents_RequiresSystemAdmin(t *testing.T) {
	require.True(t, requiresSystemAdmin(http.MethodGet, "/api/ops/outbox"))
	h := newGCHarness(t)
	r := h.do(http.MethodPost, "/api/users", CreateUserRequest{Username: "operator", Password: "Operat0rPass"}, h.admin)
	require.Less(t, r.Status, 300, string(r.Body))

	h.mustDo(http.MethodGet, "/api/ops/outbox", nil, nil, http.StatusUnauthorized)
	h.mustDo(http.MethodGet, "/api/ops/outbox", nil, basicAuth("operator", "Operat0rPass"), http.StatusForbidden)
	r = h.do(http.MethodGet, "/api/ops/outbox?state=failed&limit=10", nil, h.admin)
	require.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed}, r.Status, string(r.Body))
}

func TestHealth_ReportsHarborDegraded(t *testing.T) {
	pending.Plan(t, "2.6", "/health checks only the database")
	var mu sync.Mutex
	var contact time.Time
	h := newGCHarness(t, WithHarborContact(contactFunc(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return contact
	}), time.Minute))

	var health HealthResponse
	h.mustDo(http.MethodGet, "/health", nil, nil, http.StatusOK).decode(t, &health)
	require.Equal(t, Degraded, health.Status, "Harbor never answered")
	require.Equal(t, "degraded", health.Components["harbor"].Status)

	mu.Lock()
	contact = time.Now().Add(-time.Second)
	mu.Unlock()
	health = HealthResponse{}
	h.mustDo(http.MethodGet, "/health", nil, nil, http.StatusOK).decode(t, &health)
	require.Equal(t, Healthy, health.Status)
	require.Equal(t, "healthy", health.Components["harbor"].Status)
	require.WithinDuration(t, contact, health.Components["harbor"].LastContact, time.Second)
}

type contactFunc func() time.Time

func (f contactFunc) LastContact() time.Time { return f() }

// withHeaders sends a request and returns the status, headers and body.
func (h *gcHarness) withHeaders(method, path string, body any) (int, http.Header, []byte) {
	h.t.Helper()
	resp, err := http.DefaultClient.Do(h.newRequest(method, path, body, nil))
	require.NoError(h.t, err)
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	require.NoError(h.t, err)
	return resp.StatusCode, resp.Header, buf.Bytes()
}

func TestZtr_Returns503UntilRobotProvisioned(t *testing.T) {
	pending.Plan(t, "2.5", "registration creates the robot in the request and fails while Harbor is down")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	h.harbor.SetDown(true)

	token := h.registerSatellite("edge-01", "edge-config", "edge-group")
	status, header, body := h.withHeaders(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token})

	require.Equal(t, http.StatusServiceUnavailable, status, string(body))
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	require.NoError(t, err, "Retry-After %q", header.Get("Retry-After"))
	require.Positive(t, seconds)
	var unclaimed int
	require.NoError(t, h.db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM satellite_token WHERE token = $1 AND consumed_at IS NULL", token).Scan(&unclaimed))
	require.Equal(t, 1, unclaimed, "the satellite retries with the same token")
}

// skipHarborCheck turns on the testing-only placeholder mode. The harness
// pins SKIP_HARBOR_HEALTH_CHECK to false, and the handlers read the field
// per request.
func skipHarborCheck(t *testing.T) {
	t.Helper()
	previous := env.GC.Harbor.SkipHealthCheck
	env.GC.Harbor.SkipHealthCheck = true
	t.Cleanup(func() { env.GC.Harbor.SkipHealthCheck = previous })
}

func TestZtr_NeverReturnsPlaceholderSecret(t *testing.T) {
	pending.Plan(t, "2.6", "SKIP_HARBOR_HEALTH_CHECK makes ZTR hand out a hard-coded placeholder secret")

	t.Run("token ZTR with Harbor up returns the robot's secret", func(t *testing.T) {
		h := newGCHarness(t)
		h.createConfig("edge-config")
		h.syncGroup("edge-group", "library/alpine")
		// registerSatellite drains the outbox, so the robot exists (A92).
		token := h.registerSatellite("edge-01", "edge-config", "edge-group")
		skipHarborCheck(t)

		creds := h.ztr(token).Auth

		require.NotEqual(t, placeholderSecret, creds.Password)
		robot, ok := h.harbor.Robot(creds.Username)
		require.True(t, ok)
		require.Equal(t, robot.Secret, creds.Password)
	})

	t.Run("token ZTR with Harbor down is 503", func(t *testing.T) {
		h := newGCHarness(t)
		h.createConfig("edge-config")
		h.syncGroup("edge-group", "library/alpine")
		token := h.registerSatellite("edge-01", "edge-config", "edge-group")
		skipHarborCheck(t)
		h.harbor.SetDown(true)

		r := h.do(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil)

		require.Equal(t, http.StatusServiceUnavailable, r.Status, string(r.Body))
		require.NotContains(t, string(r.Body), placeholderSecret)
	})

	t.Run("SPIFFE ZTR with Harbor down is 503", func(t *testing.T) {
		if !spiffeBuild {
			t.Skip("SPIFFE is compiled out")
		}
		h := newGCHarness(t)
		h.createConfig("default")
		skipHarborCheck(t)
		h.harbor.SetDown(true)
		req := httptest.NewRequest(http.MethodGet, "/satellites/spiffe-ztr", nil)
		id, err := url.Parse("spiffe://harbor-satellite.local/satellite/region/default/spiffe-01")
		require.NoError(t, err)
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{id}}}}
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), placeholderSecret)
	})
}

// runWorker runs the async plane against the harness until the test ends.
// It composes the worker by hand like app.NewWorker, which this package's
// tests cannot import (app imports server).
func (h *gcHarness) runWorker() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	settings := fleet.Settings{RegistryURL: h.harbor.URL(), RobotDuration: 30 * 24 * time.Hour, TokenTTL: fleet.DefaultTokenTTL}
	must := func(err error) {
		h.t.Helper()
		require.NoError(h.t, err)
	}
	registry, err := harbor2.New(harbor2.Config{URL: h.harbor.URL(), Username: harbortest.Username, Password: harbortest.Password, Insecure: true})
	must(err)
	repo, err := postgres.NewRepository(h.db.DB)
	must(err)
	outbox, err := postgres.NewOutboxStore(h.db.DB)
	must(err)
	desired, err := postgres.NewDesiredStateStore(h.db.DB)
	must(err)
	locker, err := postgres.NewAggregateLocker(ctx, h.db.DB)
	must(err)
	sat, err := async.NewSatelliteReconciler(repo, registry, desired, settings)
	must(err)
	grp, err := async.NewGroupReconciler(repo, registry, desired, settings)
	must(err)
	cfg, err := async.NewConfigReconciler(repo, registry, desired, settings)
	must(err)
	tomb, err := async.NewTombstoneHandler(repo, registry, nil, desired)
	must(err)
	routes, err := async.NewRoutes(sat, grp, cfg, tomb)
	must(err)
	probe, err := async.NewRegistryProbe(registry, 20*time.Millisecond, time.Second)
	must(err)
	pubCfg := async.DefaultPublisherConfig()
	pubCfg.PollInterval = 20 * time.Millisecond
	pubCfg.BackoffBase = 20 * time.Millisecond
	publisher, err := async.NewPublisher(async.PublisherDeps{Store: outbox, Handler: routes, Locker: locker, Registry: probe}, pubCfg)
	must(err)
	worker, err := async.NewWorker(publisher, probe)
	must(err)
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	h.t.Cleanup(func() {
		cancel()
		require.NoError(h.t, <-done)
		require.NoError(h.t, locker.Close())
	})
}

func (h *gcHarness) pendingEvents() int {
	h.t.Helper()
	var n int
	require.NoError(h.t, h.db.QueryRowContext(context.Background(), "SELECT count(*) FROM event_outbox WHERE done_at IS NULL").Scan(&n))
	return n
}

func TestHarborDown_AdminWritesSucceedAndConverge(t *testing.T) {
	pending.Plan(t, "2.5", "admin writes call Harbor in the request and fail while it is down")
	h := newGCHarness(t)
	h.harbor.SetDown(true)

	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	token := h.registerSatellite("edge-01", "edge-config", "edge-group")
	require.Positive(t, h.pendingEvents(), "the changes wait in the outbox")

	h.harbor.SetDown(false)
	h.runWorker()
	waitFor(t, "the outbox drains", func() bool { return h.pendingEvents() == 0 })

	for _, repo := range []string{
		"satellite/config-state/edge-config/state",
		"satellite/group-state/edge-group/state",
		"satellite/satellite-state/edge-01/state",
	} {
		require.True(t, h.harbor.HasRepository(repo), repo)
	}
	require.Equal(t, []string{"library", "satellite"}, h.harbor.RobotProjects(harbortest.RobotPrefix+"edge-01"))
	h.ztr(token)
}

func TestBudgetSyncGroup_ConstantInMembers(t *testing.T) {
	pending.Plan(t, "2.5", "SyncGroup updates every member's robot and pushes the state in the request")
	measure := func(members int) usage {
		h := newGCHarness(t)
		h.syncGroup("edge-group", "library/alpine")
		h.seedGroupMembers("edge-group", members)
		return h.measure(func() { h.syncGroup("edge-group", "library/alpine", "team/app") })
	}

	one, hundred := measure(1), measure(100)

	require.Equal(t, one.statements.Total, hundred.statements.Total, "1 member:\n%s\n100 members:\n%s", one, hundred)
	for name, u := range map[string]usage{"1 member": one, "100 members": hundred} {
		require.Zero(t, u.harborAPI(), "%s: Harbor API calls in the request\n%s", name, u)
		require.Zero(t, u.registry(), "%s: registry calls in the request\n%s", name, u)
	}
}
