package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const unreachableURL = "http://127.0.0.1:0"

// staticURL wraps a literal URL string in the RegistryURLFunc shape required by
// NewHealthRegistrar, so tests don't have to write the closure every time.
func staticURL(s string) RegistryURLFunc {
	return func() (string, error) { return s, nil }
}

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

	hr := NewHealthRegistrar(staticURL(tc.registryURL), tc.gcURL, tc.headless, nil, nil)
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
	hr := NewHealthRegistrar(staticURL(""), "", false, nil, nil)

	rec := httptest.NewRecorder()
	hr.healthHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyHandler(t *testing.T) {
	registry := newStubRegistry(t)
	gc := newStubGroundControl(t)

	tests := []readyTestCase{
		{
			"all checks pass", registry.URL, gc.URL, false, true,
			http.StatusOK,
			readyChecks{Registry: "ok", GroundControl: "ok", StateSync: "ok"},
		},
		{
			"state sync pending", registry.URL, gc.URL, false, false,
			http.StatusServiceUnavailable,
			readyChecks{Registry: "ok", GroundControl: "ok", StateSync: "pending"},
		},
		{
			"ground control unavailable", registry.URL, unreachableURL, false, true,
			http.StatusServiceUnavailable,
			readyChecks{Registry: "ok", GroundControl: "unavailable", StateSync: "ok"},
		},
		{
			"registry unavailable", unreachableURL, gc.URL, false, true,
			http.StatusServiceUnavailable,
			readyChecks{Registry: "unavailable", GroundControl: "ok", StateSync: "ok"},
		},
		{
			"headless skips ground control", registry.URL, unreachableURL, true, true,
			http.StatusOK,
			readyChecks{Registry: "ok", GroundControl: "skipped", StateSync: "ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runReadyCase(t, tt)
		})
	}
}

func TestReadyHandlerHeadlessDoesNotContactGroundControl(t *testing.T) {
	gcHit := false
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gcHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gc.Close()

	registry := newStubRegistry(t)

	hr := NewHealthRegistrar(staticURL(registry.URL), gc.URL, true, nil, nil)
	hr.MarkStateSynced()

	rec := httptest.NewRecorder()
	hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	require.False(t, gcHit, "ground control should not be contacted in headless mode")
}

// TestReadyHandlerRegistryTLS proves the injected client governs TLS trust: a
// bare client rejects the self-signed registry cert, but a client that trusts
// the test server's CA succeeds. Headless skips GC so only the registry matters.
func TestReadyHandlerRegistryTLS(t *testing.T) {
	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer registry.Close()

	t.Run("bare client rejects self-signed cert", func(t *testing.T) {
		hr := NewHealthRegistrar(staticURL(registry.URL), "", true, nil, nil)
		hr.MarkStateSynced()

		rec := httptest.NewRecorder()
		hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		var resp readyResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Equal(t, "unavailable", resp.Checks.Registry)
	})

	t.Run("client trusting the CA succeeds", func(t *testing.T) {
		hr := NewHealthRegistrar(staticURL(registry.URL), "", true, registry.Client(), nil)
		hr.MarkStateSynced()

		rec := httptest.NewRecorder()
		hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		var resp readyResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Equal(t, "ok", resp.Checks.Registry)
	})
}

// TestReadyHandlerGCClientProvider proves the gcClient provider governs the GC
// check (and only the GC check): the registry uses the static client while the
// provider supplies the GC client, and a provider error surfaces as GC unavailable.
func TestReadyHandlerGCClientProvider(t *testing.T) {
	registry := newStubRegistry(t)
	gc := newStubGroundControl(t)

	t.Run("provider client is used for GC", func(t *testing.T) {
		provider := func(context.Context) (*http.Client, error) {
			return &http.Client{Timeout: 2 * time.Second}, nil
		}
		hr := NewHealthRegistrar(staticURL(registry.URL), gc.URL, false, nil, provider)
		hr.MarkStateSynced()

		rec := httptest.NewRecorder()
		hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		var resp readyResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Equal(t, "ok", resp.Checks.GroundControl)
	})

	t.Run("provider error marks GC unavailable", func(t *testing.T) {
		provider := func(context.Context) (*http.Client, error) {
			return nil, errors.New("spire unavailable")
		}
		hr := NewHealthRegistrar(staticURL(registry.URL), gc.URL, false, nil, provider)
		hr.MarkStateSynced()

		rec := httptest.NewRecorder()
		hr.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		var resp readyResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Equal(t, "ok", resp.Checks.Registry)
		require.Equal(t, "unavailable", resp.Checks.GroundControl)
	})
}
