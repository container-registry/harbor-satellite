package harborhealth

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

type config struct {
	HarborURL      string
	Timeout        time.Duration
	SkipComponents map[string]struct{}
}

func defaultConfig() *config {
	return &config{
		HarborURL: os.Getenv("HARBOR_URL"),
		Timeout:   5 * time.Second,
		SkipComponents: map[string]struct{}{
			"portal": {},
			"trivy":  {},
		},
	}
}

func CheckHealth() error {
	config := defaultConfig()
	return checkhealth(config)
}

func checkhealth(config *config) error {

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
