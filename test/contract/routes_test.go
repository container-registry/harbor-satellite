package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/satellite/state"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// satelliteCall is a request the satellite sends to Ground Control. Path is
// what arrives at the server when the satellite's base URL has no path.
type satelliteCall struct {
	name     string
	method   string
	path     string
	planItem string
	summary  string
}

var satelliteCalls = []satelliteCall{
	{name: "token ZTR", method: http.MethodPost, path: "/satellites/ztr"},
	{name: "SPIFFE ZTR", method: http.MethodGet, path: "/satellites/spiffe-ztr"},
	{
		name: "heartbeat", method: http.MethodPost, path: "//sat/sync",
		planItem: "D1", summary: "the satellite posts heartbeats to //sat/sync; GC serves /satellites/sync",
	},
	{
		name: "credential refresh", method: http.MethodPost, path: "/sat/refresh",
		planItem: "D2", summary: "GC has no route for the credential refresh the satellite posts to /sat/refresh",
	},
}

// gcRouter is Ground Control's generated router on a mux configured like
// RegisterRoutes. A middleware answers every matched request, so handlers,
// auth and the database never run and only routing decides the status.
func gcRouter() http.Handler {
	router := mux.NewRouter()
	router.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})
	return server.HandlerWithOptions(struct{ server.ServerInterface }{}, server.GorillaServerOptions{BaseRouter: router})
}

func TestGroundControlServesEverySatelliteCall(t *testing.T) {
	gc := httptest.NewServer(gcRouter())
	t.Cleanup(gc.Close)
	// Follow redirects keeping the method, as the satellite's heartbeat
	// client does.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Method = via[0].Method
		return nil
	}}

	for _, call := range satelliteCalls {
		t.Run(call.name, func(t *testing.T) {
			if call.planItem != "" {
				pending.Plan(t, call.planItem, call.summary)
			}
			req, err := http.NewRequestWithContext(context.Background(), call.method, gc.URL+call.path, nil)
			require.NoError(t, err)
			resp, err := client.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			require.Equal(t, http.StatusNoContent, resp.StatusCode,
				"%s %s ended at %s %s with %s", call.method, call.path, resp.Request.Method, resp.Request.URL.Path, resp.Status)
		})
	}
}

// TestSatelliteCallsMatchTable runs the satellite's own request code
// against a recording server, so satelliteCalls cannot drift from it.
func TestSatelliteCallsMatchTable(t *testing.T) {
	rec := newRecorder(t)
	ctx := testContext()

	t.Run("token ZTR", func(t *testing.T) {
		cm := newConfigManager(t, rec.URL(), "")
		_ = state.NewZtrProcess(cm).Execute(ctx) //nolint:errcheck // Only the request matters here.
		rec.requireLast(t, tableEntry(t, "token ZTR"))
	})

	t.Run("SPIFFE ZTR", func(t *testing.T) {
		// Driving SpiffeZtrProcess needs a SPIRE agent; its URL is the GC
		// base URL joined with this route.
		require.Equal(t, tableEntry(t, "SPIFFE ZTR").path, "/"+state.SPIFFEZeroTouchRegistrationRoute)
	})

	t.Run("heartbeat", func(t *testing.T) {
		cm := newConfigManager(t, rec.URL(), "http://harbor.test/satellite/satellite-state/edge-01/state:latest")
		log := zerolog.Nop()
		_ = state.NewStatusReportingProcess(cm, events.NewEventScheduler(&log)).Execute(ctx) //nolint:errcheck // Only the request matters here.
		rec.requireLast(t, tableEntry(t, "heartbeat"))
	})

	t.Run("credential refresh", func(t *testing.T) {
		cm := newConfigManager(t, rec.URL(), "")
		log := zerolog.Nop()
		sched, err := events.NewRefreshCredentialsEvent(cm, &log)
		require.NoError(t, err)
		runCtx, cancel := context.WithCancel(ctx)
		mark := rec.count()
		sched.Start(runCtx)
		require.Eventually(t, func() bool { return rec.count() > mark }, 10*time.Second, 10*time.Millisecond)
		cancel()
		require.NoError(t, sched.Stop(context.Background()))
		rec.requireLast(t, tableEntry(t, "credential refresh"))
	})
}

func tableEntry(t *testing.T, name string) satelliteCall {
	t.Helper()
	for _, c := range satelliteCalls {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no satelliteCalls entry %q", name)
	return satelliteCall{}
}

type recorded struct {
	method string
	path   string
}

type recorder struct {
	server *httptest.Server
	mu     sync.Mutex
	seen   []recorded
}

// newRecorder answers like a GC that accepts everything, so the satellite
// code runs to the end.
func newRecorder(t *testing.T) *recorder {
	t.Helper()
	r := &recorder{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.seen = append(r.seen, recorded{method: req.Method, path: req.URL.Path})
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck // The satellite reports a broken body itself.
			"events": []string{},
			"secret": "refreshed-secret",
			"state":  "http://harbor.test/satellite/satellite-state/edge-01/state:latest",
			"auth":   map[string]string{"username": "robot$edge-01", "password": "secret", "url": "http://harbor.test"},
		})
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *recorder) URL() string { return r.server.URL }

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}

func (r *recorder) requireLast(t *testing.T, want satelliteCall) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.seen, "the satellite sent no request")
	got := r.seen[len(r.seen)-1]
	require.Equal(t, recorded{method: want.method, path: want.path}, got, "update satelliteCalls when the satellite changes its request")
}

func newConfigManager(t *testing.T, gcURL, stateURL string) *config.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		StateConfig: config.StateConfig{
			StateURL: stateURL,
			RegistryCredentials: config.RegistryCredentials{
				Username: "robot$edge-01",
				Password: "secret",
				URL:      "http://harbor.test",
			},
		},
		AppConfig: config.AppConfig{
			GroundControlURL:  config.URL(gcURL),
			HeartbeatInterval: "@every 30s",
			UseUnsecure:       true,
		},
	}
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "token", gcURL, false, cfg)
	require.NoError(t, err)
	return cm
}

func testContext() context.Context {
	log := zerolog.Nop()
	return context.WithValue(context.Background(), logger.LoggerKey, &log)
}
