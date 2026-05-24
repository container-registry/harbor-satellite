package harborhealth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func makeServer(t *testing.T, status int, body interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func TestCheckHealth_AllHealthy(t *testing.T) {
	srv := makeServer(t, http.StatusOK, HealthResponse{
		Status: "healthy",
		Components: []Component{
			{Name: "core", Status: "healthy"},
			{Name: "database", Status: "healthy"},
		},
	})
	defer srv.Close()

	cfg := &config{
		HarborURL:      srv.URL,
		Timeout:        defaultConfig().Timeout,
		SkipComponents: map[string]struct{}{},
	}
	if err := checkhealth(cfg); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestCheckHealth_UnhealthyComponent(t *testing.T) {
	srv := makeServer(t, http.StatusOK, HealthResponse{
		Status: "unhealthy",
		Components: []Component{
			{Name: "core", Status: "healthy"},
			{Name: "database", Status: "unhealthy", Error: "connection refused"},
		},
	})
	defer srv.Close()

	cfg := &config{
		HarborURL:      srv.URL,
		Timeout:        defaultConfig().Timeout,
		SkipComponents: map[string]struct{}{},
	}
	if err := checkhealth(cfg); err == nil {
		t.Error("expected error for unhealthy component, got nil")
	}
}

func TestCheckHealth_SkippedComponentNotReported(t *testing.T) {
	srv := makeServer(t, http.StatusOK, HealthResponse{
		Status: "unhealthy",
		Components: []Component{
			{Name: "trivy", Status: "unhealthy", Error: "not running"},
			{Name: "core", Status: "healthy"},
		},
	})
	defer srv.Close()

	cfg := &config{
		HarborURL: srv.URL,
		Timeout:   defaultConfig().Timeout,
		SkipComponents: map[string]struct{}{
			"trivy": {},
		},
	}
	if err := checkhealth(cfg); err != nil {
		t.Errorf("expected skipped component to be ignored, got: %v", err)
	}
}

func TestCheckHealth_Non200Status(t *testing.T) {
	srv := makeServer(t, http.StatusServiceUnavailable, map[string]string{"error": "down"})
	defer srv.Close()

	cfg := &config{
		HarborURL:      srv.URL,
		Timeout:        defaultConfig().Timeout,
		SkipComponents: map[string]struct{}{},
	}
	if err := checkhealth(cfg); err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}

func TestCheckHealth_InvalidURL(t *testing.T) {
	cfg := &config{
		HarborURL:      "not-a-url",
		Timeout:        defaultConfig().Timeout,
		SkipComponents: map[string]struct{}{},
	}
	if err := checkhealth(cfg); err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestCheckHealth_UnsupportedScheme(t *testing.T) {
	cfg := &config{
		HarborURL:      "ftp://harbor.example.com",
		Timeout:        defaultConfig().Timeout,
		SkipComponents: map[string]struct{}{},
	}
	if err := checkhealth(cfg); err == nil {
		t.Error("expected error for unsupported scheme, got nil")
	}
}

func TestCheckHealth_SkipEnvVar(t *testing.T) {
	t.Setenv("SKIP_HARBOR_HEALTH_CHECK", "true")
	if err := CheckHealth(); err != nil {
		t.Errorf("expected no error when skip env var set, got: %v", err)
	}
}

func TestDefaultConfig_UsesEnvVar(t *testing.T) {
	os.Setenv("HARBOR_URL", "http://harbor.test")
	defer os.Unsetenv("HARBOR_URL")
	cfg := defaultConfig()
	if cfg.HarborURL != "http://harbor.test" {
		t.Errorf("expected HARBOR_URL from env, got: %s", cfg.HarborURL)
	}
}

func TestComponentIsHealthy(t *testing.T) {
	c := Component{Name: "core", Status: "healthy"}
	if !c.IsHealthy() {
		t.Error("expected healthy component to return true")
	}
	c.Status = "unhealthy"
	if c.IsHealthy() {
		t.Error("expected unhealthy component to return false")
	}
}

func TestGetUnhealthyComponents_ReturnsNamesAndErrors(t *testing.T) {
	h := HealthResponse{
		Components: []Component{
			{Name: "core", Status: "healthy"},
			{Name: "database", Status: "unhealthy", Error: "timeout"},
		},
	}
	result := h.GetUnhealthyComponents(map[string]struct{}{})
	if len(result) != 2 {
		t.Errorf("expected 2 entries (name + error), got %d", len(result))
	}
	if result[0] != "database" {
		t.Errorf("expected database, got %s", result[0])
	}
	if result[1] != "timeout" {
		t.Errorf("expected timeout, got %s", result[1])
	}
}
