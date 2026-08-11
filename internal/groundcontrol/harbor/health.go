package harbor

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
)

type Component struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (c *Component) IsHealthy() bool {
	return c.Status == "healthy"
}

type HealthResponse struct {
	Components []Component `json:"components"`
	Status     string      `json:"status"`
}

func (h *HealthResponse) GetUnhealthyComponents(skip map[string]struct{}) []string {
	var unhealthy []string
	for _, c := range h.Components {
		if _, ignore := skip[c.Name]; ignore {
			continue
		}
		if !c.IsHealthy() {
			unhealthy = append(unhealthy, c.Name, c.Error)
		}
	}
	return unhealthy
}

type healthConfig struct {
	HarborURL      string
	Timeout        time.Duration
	SkipComponents map[string]struct{}
}

func defaultHealthConfig() *healthConfig {
	return &healthConfig{
		HarborURL: env.GC.Harbor.URL,
		Timeout:   5 * time.Second,
		SkipComponents: map[string]struct{}{
			"portal":      {},
			"trivy":       {},
			"registryctl": {},
			"jobservice":  {},
		},
	}
}

func CheckHealth() error {
	cfg := env.GC
	// Allow skipping health check for development/testing
	if cfg.Harbor.SkipHealthCheck {
		log.Println("WARNING: Harbor health check skipped (SKIP_HARBOR_HEALTH_CHECK=true)")
		return nil
	}

	config := defaultHealthConfig()
	return checkhealth(config)
}

func checkhealth(config *healthConfig) error {
	parsed, err := url.ParseRequestURI(config.HarborURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s (must be http or https)", parsed.Scheme)
	}

	client := &http.Client{
		Timeout: config.Timeout,
	}

	resp, err := client.Get(config.HarborURL + "/api/v2.0/health")
	if err != nil {
		return fmt.Errorf("failed to call API: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	unhealthyComponents := health.GetUnhealthyComponents(config.SkipComponents)

	if len(unhealthyComponents) > 0 {
		return fmt.Errorf("unhealthy components: %v", unhealthyComponents)
	}
	return nil
}
