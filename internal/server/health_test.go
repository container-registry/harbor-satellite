package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const unreachableURL = "http://127.0.0.1:0"

// newStubRegistry returns a test server answering the OCI base endpoint with 401.
func newStubRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newStubGroundControl returns a test server answering /health with 200.
func newStubGroundControl(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type readyTestCase struct {
	name        string
	registryURL string
	gcURL       string
	headless    bool
	synced      bool
	wantCode    int
	wantChecks  readyChecks
}

// runReadyCase issues a /ready request for the case and asserts the result.
// The expected status string is derived from wantCode (200 -> ready).
func runReadyCase(t *testing.T, tc readyTestCase) {
	t.Helper()

	hr := NewHealthRegistrar(tc.registryURL, tc.gcURL, tc.headless)
	if tc.synced {
		hr.MarkStateSynced()
	}

	rec := httptest.NewRecorder()
	hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	require.Equal(t, tc.wantCode, rec.Code)

	wantStatus := "ready"
	if tc.wantCode != http.StatusOK {
		wantStatus = "not_ready"
	}

	var resp readyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, wantStatus, resp.Status)
	require.Equal(t, tc.wantChecks, resp.Checks)
}

func TestHealthHandlerAlwaysOK(t *testing.T) {
	hr := NewHealthRegistrar("", "", false)

	rec := httptest.NewRecorder()
	hr.healthHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyHandler(t *testing.T) {
	registry := newStubRegistry(t)
	gc := newStubGroundControl(t)

	tests := []readyTestCase{
		{"all checks pass", registry.URL, gc.URL, false, true,
			http.StatusOK, readyChecks{Registry: "ok", GroundControl: "ok", StateSync: "ok"}},
		{"state sync pending", registry.URL, gc.URL, false, false,
			http.StatusServiceUnavailable, readyChecks{Registry: "ok", GroundControl: "ok", StateSync: "pending"}},
		{"ground control unavailable", registry.URL, unreachableURL, false, true,
			http.StatusServiceUnavailable, readyChecks{Registry: "ok", GroundControl: "unavailable", StateSync: "ok"}},
		{"registry unavailable", unreachableURL, gc.URL, false, true,
			http.StatusServiceUnavailable, readyChecks{Registry: "unavailable", GroundControl: "ok", StateSync: "ok"}},
		{"headless skips ground control", registry.URL, unreachableURL, true, true,
			http.StatusOK, readyChecks{Registry: "ok", GroundControl: "skipped", StateSync: "ok"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runReadyCase(t, tt)
		})
	}
}

func TestReadyHandlerHeadlessDoesNotContactGroundControl(t *testing.T) {
	gcHit := false
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gcHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gc.Close()

	registry := newStubRegistry(t)

	hr := NewHealthRegistrar(registry.URL, gc.URL, true)
	hr.MarkStateSynced()

	rec := httptest.NewRecorder()
	hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	require.False(t, gcHit, "ground control should not be contacted in headless mode")
}

I've opened #463 for #240, adding /health and /ready for the satellite so Kubernetes can gate workloads on the satellite being ready, keeping pods from pulling before images are proxied and avoiding ImagePullBackOff churn. Two things I'd like your input on: 
1. TLS — the readiness checks hit local Zot and GC with a bare http.Client, unlike the rest of the code which uses createHTTPClient(cm.GetTLSConfig(), cm.UseUnsecure()), so it honors neither UseUnsecure nor a custom CA; if a registry or GC is served over HTTPS with a self-signed/custom-CA cert the check fails verification and /ready falsely reports "unavailable" — is the observability port meant to stay internal-only (bare client fine), or should these checks honor the satellite's TLS config?
2. Headless — the issue mentions skipping the GC check in headless mode, but no headless concept exists today and I've only made /ready skip the GC check; should we add a real mode to run the satellite without GC, and is "headless" meant to describe the satellite running standalone or just GC being absent?
