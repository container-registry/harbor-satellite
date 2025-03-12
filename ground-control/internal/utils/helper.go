package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	m "container-registry.com/harbor-satellite/ground-control/internal/models"
	"container-registry.com/harbor-satellite/ground-control/reg/harbor"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

// GetProjectNames parses artifacts & returns project names
func GetProjectNames(artifacts *[]m.Artifact) []string {
	uniqueProjects := make(map[string]struct{}) // Map to track unique project names
	var projects []string

	for _, artifact := range *artifacts {
		if artifact.Deleted {
			continue
		}
		// Extract project name from repository
		project := strings.Split(artifact.Repository, "/")[0]

		// Check if the project is already added
		if _, exists := uniqueProjects[project]; !exists {
			uniqueProjects[project] = struct{}{}
			projects = append(projects, project)
		}
	}

	return projects
}

// Create robot account for satellites
func CreateRobotAccForSatellite(ctx context.Context, projects []string, name string) (*models.RobotCreated, error) {
	robotTemp := harbor.RobotAccountTemplate(name, projects)
	robt, err := harbor.CreateRobotAccount(ctx, robotTemp)
	if err != nil {
		return nil, fmt.Errorf("error creating robot account: %w", err)
	}

	return robt.Payload, nil
}

// Update robot account
func UpdateRobotProjects(ctx context.Context, projects []string, id string) (*robot.UpdateRobotOK, error) {
	ID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error invalid ID: %w", err)
	}
	robot, err := harbor.GetRobotAccount(ctx, ID)
	if err != nil {
		return nil, fmt.Errorf("error getting robot account: %w", err)
	}

	// satellites should always have permission to satellite project by default
	// to get state artifacts
	projects = append(projects, "satellite")

	robot.Permissions = harbor.GenRobotPerms(projects)

	updated, err := harbor.UpdateRobotAccount(ctx, robot)
	if err != nil {
		return nil, fmt.Errorf("error updating robot account: %w", err)
	}

	return updated, nil
}

func AssembleGroupState(groupName string) string {
	state := fmt.Sprintf("%s/satellite/group-state/%s/state:latest", os.Getenv("HARBOR_URL"), groupName)
	return state
}

// Create State Artifact for group
func CreateStateArtifact(stateArtifact *m.StateArtifact) error {
	// Set the registry URL from environment variable
	stateArtifact.Registry = os.Getenv("HARBOR_URL")
	if stateArtifact.Registry == "" {
		return fmt.Errorf("HARBOR_URL environment variable is not set")
	}

	// Marshal the state artifact to JSON format
	data, err := json.Marshal(stateArtifact)
	if err != nil {
		return fmt.Errorf("failed to marshal state artifact to JSON: %v", err)
	}

	// Create the image with the state artifact JSON
	img, err := crane.Image(map[string][]byte{"artifacts.json": data})
	if err != nil {
		return fmt.Errorf("failed to create image: %v", err)
	}

	// Configure repository and credentials
	repo := fmt.Sprintf("satellite/group-state/%s", stateArtifact.Group)
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		return fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})
	options := []crane.Option{crane.WithAuth(auth)}

	// Construct the destination repository and strip protocol, if present
	destinationRepo := getStateArtifactDestination(stateArtifact.Registry, repo)
	if strings.Contains(destinationRepo, "://") {
		destinationRepo = strings.SplitN(destinationRepo, "://", 2)[1]
	}

	// Push the image to the repository
	if err := crane.Push(img, destinationRepo, options...); err != nil {
		return fmt.Errorf("failed to push image: %v", err)
	}

	// Tag the image with timestamp and latest tags
	tags := []string{fmt.Sprintf("%d", time.Now().Unix()), "latest"}
	for _, tag := range tags {
		if err := crane.Tag(destinationRepo, tag, options...); err != nil {
			return fmt.Errorf("failed to tag image with %s: %v", tag, err)
		}
	}

	return nil
}

func CreateOrUpdateSatStateArtifact(satelliteName string, states []string) error {
	if len(states) <= 0 {
		return nil
	}
	// Set the registry URL from environment variable
	registry := os.Getenv("HARBOR_URL")
	if registry == "" {
		return fmt.Errorf("HARBOR_URL environment variable is not set")
	}

	// Create the state artifact
	satelliteState := &m.SatelliteStateArtifact{
		States: states,
	}

	// Marshal the satellite state artifact to JSON format
	data, err := json.Marshal(satelliteState)
	if err != nil {
		return fmt.Errorf("failed to marshal satellite state artifact to JSON: %v", err)
	}

	// Create the image with the satellite state artifact JSON
	img, err := crane.Image(map[string][]byte{"artifacts.json": data})
	if err != nil {
		return fmt.Errorf("failed to create image: %v", err)
	}

	// Configure repository and credentials
	repo := fmt.Sprintf("satellite/satellite-state/%s", satelliteName)
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		return fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})
	options := []crane.Option{crane.WithAuth(auth)}

	// Construct the destination repository and strip protocol, if present
	destinationRepo := getStateArtifactDestination(registry, repo)
	if strings.Contains(destinationRepo, "://") {
		destinationRepo = strings.SplitN(destinationRepo, "://", 2)[1]
	}

	// Push the image to the repository
	if err := crane.Push(img, destinationRepo, options...); err != nil {
		return fmt.Errorf("failed to push image: %v", err)
	}

	// Tag the image with timestamp and latest tags
	tags := []string{fmt.Sprintf("%d", time.Now().Unix()), "latest"}
	for _, tag := range tags {
		if err := crane.Tag(destinationRepo, tag, options...); err != nil {
			return fmt.Errorf("failed to tag image with %s: %v", tag, err)
		}
	}

	return nil
}

func DeleteSatelliteStateArtifact(satelliteName string) error {
	// Set the registry URL from environment variable
	registry := os.Getenv("HARBOR_URL")
	if registry == "" {
		return fmt.Errorf("HARBOR_URL environment variable is not set")
	}

	// Configure repository and credentials
	repo := fmt.Sprintf("satellite/satellite-state/%s", satelliteName)
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		return fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})
	options := []crane.Option{crane.WithAuth(auth)}

	// Construct the destination repository and strip protocol, if present
	destinationRepo := getStateArtifactDestination(registry, repo)
	if strings.Contains(destinationRepo, "://") {
		destinationRepo = strings.SplitN(destinationRepo, "://", 2)[1]
	}

	if strings.Contains(destinationRepo, "://") {
		destinationRepo = strings.SplitN(destinationRepo, "://", 2)[1]
	}

	// Delete the image from the repository
	if err := crane.Delete(destinationRepo, options...); err != nil {
		return fmt.Errorf("failed to delete satellite state artifact: %v", err)
	}

	return nil
}

func AssembleSatelliteState(satelliteName string) string {
	return fmt.Sprintf("%s/satellite/satellite-state/%s/state:latest", os.Getenv("HARBOR_URL"), satelliteName)
}

func getStateArtifactDestination(registry, repository string) string {
	return fmt.Sprintf("%s/%s/%s", registry, repository, "state")
}
