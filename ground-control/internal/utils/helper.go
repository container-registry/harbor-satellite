package utils

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"container-registry.com/harbor-satellite/ground-control/reg"
	"container-registry.com/harbor-satellite/ground-control/reg/harbor"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

// ParseArtifactURL parses an artifact URL and returns a reg.Images struct
func ParseArtifactURL(rawURL string) (reg.Images, error) {
	// Add "https://" scheme if missing
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	// Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return reg.Images{}, fmt.Errorf("error: invalid URL: %s", err)
	}

	// Extract registry (host) and repo path
	registry := parsedURL.Host
	path := strings.TrimPrefix(parsedURL.Path, "/")

	// Split the repo, tag, and digest
	repo, tag, digest := splitRepoTagDigest(path)

	// Validate that repository and registry exist
	if repo == "" || registry == "" {
		return reg.Images{}, fmt.Errorf("error: missing repository or registry in URL: %s", rawURL)
	}

	// Validate that either tag or digest exists
	if tag == "" && digest == "" {
		return reg.Images{}, fmt.Errorf("error: missing tag or digest in artifact URL: %s", rawURL)
	}

	// Return a populated reg.Images struct
	return reg.Images{
		Registry:   registry,
		Repository: repo,
		Tag:        tag,
		Digest:     digest,
	}, nil
}

// Helper function to split repo, tag, and digest from the path
func splitRepoTagDigest(path string) (string, string, string) {
	var repo, tag, digest string

	// First, split based on '@' to separate digest
	if strings.Contains(path, "@") {
		parts := strings.Split(path, "@")
		repo = parts[0]
		digest = parts[1]
	} else {
		repo = path
	}

	// Next, split repo and tag based on ':'
	if strings.Contains(repo, ":") {
		parts := strings.Split(repo, ":")
		repo = parts[0]
		tag = parts[1]
	}

	return repo, tag, digest
}

// Create robot account for satellites
func CreateRobotAccForSatellite(ctx context.Context, repos []string, name string) (*models.RobotCreated, error) {
	// get harbor client
	harborClient, err := harbor.GetClient()
	if err != nil {
		return nil, fmt.Errorf("error getting Harbor client: %w", err)
	}

	var projects []string
	// get projects from repos
	for _, project := range repos {
		proj := strings.Split(project, "/")
		projects = append(projects, proj[0])
	}

	robotTemp := harbor.RobotAccountTemplate(name, projects)

	robot, err := harbor.CreateRobotAccount(ctx, robotTemp, harborClient)
	if err != nil {
		return nil, fmt.Errorf("error creating robot account: %w", err)
	}

	return robot.Payload, nil
}
