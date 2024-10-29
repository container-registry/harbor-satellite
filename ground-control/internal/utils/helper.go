package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	m "container-registry.com/harbor-satellite/ground-control/internal/models"
	"container-registry.com/harbor-satellite/ground-control/reg"
	"container-registry.com/harbor-satellite/ground-control/reg/harbor"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

// GetProjectNames parses artifacts & returns project names
func GetProjectNames(artifacts *[]m.Artifact) []string {
	var projects []string
	for _, artifact := range *artifacts {
		if artifact.Deleted {
			continue
		}
		project := strings.Split(artifact.Repository, "/")
		projects = append(projects, project[0])
	}
	return projects
}

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

// Update robot account
func UpdateRobotProjects(ctx context.Context, projects []string, name string, id string) (*robot.UpdateRobotOK, error) {
	ID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error invalid ID: %w", err)
	}
	// get harbor client
	harborClient, err := harbor.GetClient()
	if err != nil {
		return nil, fmt.Errorf("error getting Harbor client: %w", err)
	}

	robot, err := harbor.GetRobotAccount(ctx, ID, harborClient)
	if err != nil {
		return nil, fmt.Errorf("error getting robot account: %w", err)
	}

	robot.Permissions = harbor.GenRobotPerms(projects)

	updated, err := harbor.UpdateRobotAccount(ctx, robot, harborClient)
	if err != nil {
		return nil, fmt.Errorf("error updating robot account: %w", err)
	}

	return updated, nil
}

func AssembleGroupState(groupName string) string {
	state := fmt.Sprintf("%s/satellite/%s/state:latest", os.Getenv("HARBOR_URL"), groupName)
	return state
}

// Create State Artifact for group
func CreateStateArtifact(stateArtifact *m.StateArtifact) error {
	result := stateArtifact
	result.Registry = os.Getenv("HARBOR_URL")

	data, err := json.Marshal(result)
	if err != nil {
		log.Println("failed to marshal state.json")
		log.Println(err)
		return nil
	}
	// create the state json in registry and make the satellite follow that.
	img, err := crane.Image(map[string][]byte{
		"artifacts.json": data,
	})
	if err != nil {
		return fmt.Errorf("image create failed: %v", err)
	}
	repo := fmt.Sprintf("satellite/%s", stateArtifact.Group)

	// Get credentials from environment variables
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		log.Fatalln("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
		return fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})
	options := []crane.Option{crane.WithAuth(auth)}
	destinationRepo := fmt.Sprintf("%s/%s", path.Dir(repo), "state")
	err = crane.Push(img, destinationRepo, options...)
	if err != nil {
		return fmt.Errorf("push image failed: %v", err)
	}
	err = crane.Tag(destinationRepo, fmt.Sprintf("%d", time.Now().Unix()), options...)
	if err != nil {
		return fmt.Errorf("tag image failed: %v", err)
	}
	err = crane.Tag(destinationRepo, "latest", options...)
	if err != nil {
		return fmt.Errorf("tag image failed: %v", err)
	}

	return nil
}
