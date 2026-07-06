package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbor"
	m "github.com/container-registry/harbor-satellite/internal/groundcontrol/models"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
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
	robotID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error invalid ID: %w", err)
	}
	robot, err := harbor.GetRobotAccount(ctx, robotID)
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
	state := fmt.Sprintf("%s/satellite/group-state/%s/state:latest", env.GC.Harbor.URL, groupName)
	return state
}

// Create State Artifact for group
func CreateStateArtifact(ctx context.Context, stateArtifact *m.StateArtifact) error {
	cfg := env.GC.Harbor
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Set the registry URL from environment variable
	stateArtifact.Registry = cfg.URL

	// Marshal the state artifact to JSON format
	data, err := json.Marshal(stateArtifact)
	if err != nil {
		return fmt.Errorf("failed to marshal state artifact to JSON: %w", err)
	}

	// Create the image with the state artifact JSON
	img, err := crane.Image(map[string][]byte{"artifacts.json": data})
	if err != nil {
		return fmt.Errorf("failed to create image: %w", err)
	}

	// Configure repository and credentials
	repo := fmt.Sprintf("satellite/group-state/%s", stateArtifact.Group)

	auth := authn.FromConfig(authn.AuthConfig{
		Username: cfg.Username,
		Password: cfg.Password,
	})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}

	if strings.HasPrefix(stateArtifact.Registry, "http://") {
		options = append(options, crane.Insecure)
	}

	// Construct the destination repository and strip protocol, if present
	destinationRepo := getStateArtifactDestination(stateArtifact.Registry, repo)
	if strings.Contains(destinationRepo, "://") {
		destinationRepo = strings.SplitN(destinationRepo, "://", 2)[1]
	}

	// Push the image to the repository
	if err := crane.Push(img, destinationRepo, options...); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	// Tag the image with timestamp and latest tags
	tags := []string{fmt.Sprintf("%d", time.Now().Unix()), "latest"}
	for _, tag := range tags {
		if err := crane.Tag(destinationRepo, tag, options...); err != nil {
			return fmt.Errorf("failed to tag image with %s: %w", tag, err)
		}
	}

	return nil
}

// Create and Push State Artifact for Config
func CreateAndPushConfigStateArtifact(ctx context.Context, configData []byte, configName string) error {
	// func CreateAndPushConfigStateArtifact(ctx context.Context, configObject *m.ConfigObject) error {
	// Marshal the state artifact to JSON format
	// configData, err := json.Marshal(configObject.Config)
	// if err != nil {
	// 	return fmt.Errorf("failed to marshal state artifact to JSON: %v", err)
	// }

	// Create the image with the state artifact JSON
	img, err := crane.Image(map[string][]byte{"artifacts.json": configData})
	if err != nil {
		return fmt.Errorf("failed to create image: %w", err)
	}

	if err := env.GC.Harbor.Validate(); err != nil {
		return err
	}

	cfg := env.GC.Harbor
	auth := authn.FromConfig(authn.AuthConfig{Username: cfg.Username, Password: cfg.Password})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}

	if strings.HasPrefix(cfg.URL, "http://") {
		options = append(options, crane.Insecure)
	}
	// Construct the destination repository and strip protocol, if present
	destinationRepo := AssembleConfigState(configName)
	destinationRepo = stripProtocol(destinationRepo)

	// Push the image to the repository
	if err := crane.Push(img, destinationRepo, options...); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	return tagImage(destinationRepo, options)
}

func AssembleSatelliteState(satelliteName string) string {
	return fmt.Sprintf("%s/satellite/satellite-state/%s/state:latest", env.GC.Harbor.URL, satelliteName)
}

func AssembleConfigState(configName string) string {
	return fmt.Sprintf("%s/satellite/config-state/%s/state:latest", env.GC.Harbor.URL, configName)
}

func CreateOrUpdateSatStateArtifact(ctx context.Context, satelliteName string, states []string, config string) error {
	if satelliteName == "" {
		return fmt.Errorf("the satellite name must be atleast one character long")
	}

	if len(states) == 0 {
		return nil
	}

	if err := env.GC.Harbor.Validate(); err != nil {
		return err
	}

	satelliteState := &m.SatelliteStateArtifact{States: states, Config: AssembleConfigState(config)}
	data, err := json.Marshal(satelliteState)
	if err != nil {
		return fmt.Errorf("failed to marshal satellite state artifact to JSON: %w", err)
	}

	img, err := crane.Image(map[string][]byte{"artifacts.json": data})
	if err != nil {
		return fmt.Errorf("failed to create image: %w", err)
	}

	cfg := env.GC.Harbor
	auth := authn.FromConfig(authn.AuthConfig{Username: cfg.Username, Password: cfg.Password})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}
	if strings.HasPrefix(cfg.URL, "http://") {
		options = append(options, crane.Insecure)
	}

	destinationRepo := AssembleSatelliteState(satelliteName)
	destinationRepo = stripProtocol(destinationRepo)

	if err := pushImage(img, destinationRepo, options); err != nil {
		return err
	}

	return tagImage(destinationRepo, options)
}

func DeleteArtifact(deleteURL string) error {
	if err := env.GC.Harbor.Validate(); err != nil {
		return err
	}
	cfg := env.GC.Harbor

	req, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(cfg.Username, cfg.Password)
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete repository, received status: %d", resp.StatusCode)
	}

	return nil
}

func ConstructHarborDeleteURL(repo string, repoType string) string {
	repositoryName := fmt.Sprintf("%s-state/%s/state", repoType, repo)
	doubleEncodedRepoName := url.QueryEscape(url.QueryEscape(repositoryName))
	return fmt.Sprintf("%s/api/v2.0/projects/satellite/repositories/%s", env.GC.Harbor.URL, doubleEncodedRepoName)
}

func stripProtocol(url string) string {
	if strings.Contains(url, "://") {
		return strings.SplitN(url, "://", 2)[1]
	}
	return url
}

func pushImage(img v1.Image, destination string, options []crane.Option) error {
	if err := crane.Push(img, destination, options...); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}
	return nil
}

func tagImage(destination string, options []crane.Option) error {
	tags := []string{fmt.Sprintf("%d", time.Now().Unix()), "latest"}
	for _, tag := range tags {
		if err := crane.Tag(destination, tag, options...); err != nil {
			return fmt.Errorf("failed to tag image with %s: %w", tag, err)
		}
	}
	return nil
}

func getStateArtifactDestination(registry, repository string) string {
	return fmt.Sprintf("%s/%s/%s", registry, repository, "state")
}

// IsValidName validates if a name meets the requirements:
// 1. 1-255 characters long
// 2. Only lowercase characters, numbers, and ._- are allowed
// 3. Must start with a letter or number
func IsValidName(name string) bool {
	if len(name) < 1 || len(name) > 255 {
		return false
	}

	pattern := `^[a-z0-9][a-z0-9._-]*$`
	matched, err := regexp.MatchString(pattern, name)
	if err != nil {
		return false
	}
	return matched
}
