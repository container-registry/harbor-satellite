package harborhealth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

func harbor(t *testing.T, status int, body any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2.0/health" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		switch b := body.(type) {
		case string:
			if _, err := w.Write([]byte(b)); err != nil {
				t.Error(err)
			}
		default:
			require.NoError(t, json.NewEncoder(w).Encode(b))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func components(c ...Component) HealthResponse {
	return HealthResponse{Status: "healthy", Components: c}
}

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   any
		err    string
	}{
		{
			name:   "all healthy",
			status: http.StatusOK,
			body:   components(Component{Name: "core", Status: "healthy"}, Component{Name: "database", Status: "healthy"}),
		},
		{
			name:   "skipped components may be unhealthy",
			status: http.StatusOK,
			body: components(
				Component{Name: "core", Status: "healthy"},
				Component{Name: "trivy", Status: "unhealthy"},
				Component{Name: "jobservice", Status: "unhealthy"},
				Component{Name: "portal", Status: "unhealthy"},
				Component{Name: "registryctl", Status: "unhealthy"},
			),
		},
		{
			name:   "an unhealthy required component names it and its error",
			status: http.StatusOK,
			body:   components(Component{Name: "core", Status: "healthy"}, Component{Name: "database", Status: "unhealthy", Error: "connection refused"}),
			err:    "unhealthy components: [database connection refused]",
		},
		{
			name:   "non-200 status",
			status: http.StatusServiceUnavailable,
			body:   "{}",
			err:    "unexpected HTTP status: 503 Service Unavailable",
		},
		{
			name:   "body that is not JSON",
			status: http.StatusOK,
			body:   "<html>",
			err:    "failed to parse response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := harbor(t, tt.status, tt.body)
			cfg := defaultConfig()
			cfg.HarborURL = srv.URL
			err := checkhealth(context.Background(), cfg)
			if tt.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.err)
		})
	}
}

func TestCheckHealthRejectsBadURLs(t *testing.T) {
	for url, want := range map[string]string{
		"":                  "invalid URL format",
		"harbor.local":      "invalid URL format",
		"ftp://harbor.test": "unsupported URL scheme: ftp",
	} {
		cfg := defaultConfig()
		cfg.HarborURL = url
		require.ErrorContains(t, checkhealth(context.Background(), cfg), want, url)
	}
}

func TestCheckHealthUnreachableHarbor(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	cfg := defaultConfig()
	cfg.HarborURL = url
	require.ErrorContains(t, checkhealth(context.Background(), cfg), "failed to call API")
}

func TestCheckHealthTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	cfg := defaultConfig()
	cfg.HarborURL = srv.URL
	cfg.Timeout = 50 * time.Millisecond
	start := time.Now()
	require.ErrorContains(t, checkhealth(context.Background(), cfg), "failed to call API")
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestCheckHealthUsesTheEnvironment(t *testing.T) {
	prev := env.GC.Harbor
	t.Cleanup(func() { env.GC.Harbor = prev })

	env.GC.Harbor.SkipHealthCheck = true
	env.GC.Harbor.URL = "not a url"
	require.NoError(t, CheckHealth(context.Background()), "a skipped check never looks at the URL")

	env.GC.Harbor.SkipHealthCheck = false
	require.ErrorContains(t, CheckHealth(context.Background()), "invalid URL format")

	srv := harbor(t, http.StatusOK, components(Component{Name: "core", Status: "healthy"}))
	env.GC.Harbor.URL = srv.URL
	require.NoError(t, CheckHealth(context.Background()))
}
