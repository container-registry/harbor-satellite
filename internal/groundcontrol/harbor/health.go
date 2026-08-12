package harbor

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/container-registry/harbor-satellite/internal/env"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/health"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

const defaultHealthCheckTimeout = 5 * time.Second

var ignoredHealthComponents = map[string]struct{}{
	"portal":      {},
	"trivy":       {},
	"registryctl": {},
	"jobservice":  {},
}

// CheckHealth verifies that all required Harbor components are healthy.
func CheckHealth() error {
	if env.GC.Harbor.SkipHealthCheck {
		log.Println("WARNING: Harbor health check skipped (SKIP_HARBOR_HEALTH_CHECK=true)")
		return nil
	}

	harborURL, err := url.Parse(env.GC.Harbor.URL)
	if err != nil {
		return fmt.Errorf("parse Harbor URL: %w", err)
	}

	client := v2client.New(v2client.Config{URL: harborURL})
	return checkHealth(client.Health)
}

func checkHealth(client health.API) error {
	params := health.NewGetHealthParamsWithTimeout(defaultHealthCheckTimeout)
	response, err := client.GetHealth(context.Background(), params)
	if err != nil {
		return fmt.Errorf("failed to get Harbor health: %w", err)
	}
	if response == nil || response.Payload == nil {
		return fmt.Errorf("failed to get Harbor health: empty response")
	}

	unhealthy := getUnhealthyComponents(response.Payload.Components, ignoredHealthComponents)
	if len(unhealthy) > 0 {
		return fmt.Errorf("unhealthy components: %v", unhealthy)
	}

	return nil
}

func getUnhealthyComponents(components []*models.ComponentHealthStatus, ignored map[string]struct{}) []string {
	var unhealthy []string
	for _, component := range components {
		if component == nil {
			continue
		}
		if _, ignore := ignored[component.Name]; ignore {
			continue
		}
		if component.Status != "healthy" {
			entry := component.Name
			if component.Error != "" {
				entry += ": " + component.Error
			}
			unhealthy = append(unhealthy, entry)
		}
	}

	return unhealthy
}
