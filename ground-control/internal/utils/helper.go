package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	m "github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

var (
	registry = os.Getenv("HARBOR_URL")
	username = os.Getenv("HARBOR_USERNAME")
	password = os.Getenv("HARBOR_PASSWORD")
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
func CreateStateArtifact(ctx context.Context, stateArtifact *m.StateArtifact) error {
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
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}

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

// Create State Artifact for Config
func CreateConfigStateArtifact(ctx context.Context, configObject *m.ConfigObject) error {
	// Marshal the state artifact to JSON format
	configData, err := json.Marshal(configObject.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal state artifact to JSON: %v", err)
	}

	// Create the image with the state artifact JSON
	img, err := crane.Image(map[string][]byte{"artifacts.json": configData})
	if err != nil {
		return fmt.Errorf("failed to create image: %v", err)
	}

	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		return fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}

	// Construct the destination repository and strip protocol, if present
	destinationRepo := AssembleConfigState(configObject.ConfigName)
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

func AssembleSatelliteState(satelliteName string) string {
	return fmt.Sprintf("%s/satellite/satellite-state/%s/state:latest", os.Getenv("HARBOR_URL"), satelliteName)
}

func AssembleConfigState(configName string) string {
	return fmt.Sprintf("%s/satellite/config-state/%s/state:latest", os.Getenv("HARBOR_URL"), configName)
}

func CreateOrUpdateSatStateArtifact(ctx context.Context, satelliteName string, states []string, config string) error {
	if satelliteName == "" {
		return fmt.Errorf("the satellite name must be atleast one character long")
	}

	if len(states) == 0 {
		return nil
	}

	if err := envSanityCheck(); err != nil {
		return err
	}

	satelliteState := &m.SatelliteStateArtifact{States: states, Config: AssembleConfigState(config)}
	data, err := json.Marshal(satelliteState)
	if err != nil {
		return fmt.Errorf("failed to marshal satellite state artifact to JSON: %v", err)
	}

	img, err := crane.Image(map[string][]byte{"artifacts.json": data})
	if err != nil {
		return fmt.Errorf("failed to create image: %v", err)
	}

	auth := authn.FromConfig(authn.AuthConfig{Username: username, Password: password})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}

	destinationRepo := AssembleSatelliteState(satelliteName)
	destinationRepo = stripProtocol(destinationRepo)

	if err := pushImage(img, destinationRepo, options); err != nil {
		return err
	}

	return tagImage(destinationRepo, options)
}

func DeleteArtifact(deleteURL string) error {
	if err := envSanityCheck(); err != nil {
		return err
	}

	req, err := http.NewRequest("DELETE", deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.SetBasicAuth(username, password)
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete repository, received status: %d", resp.StatusCode)
	}

	return nil
}

func ConstructHarborDeleteURL(repo string, repoType string) string {
	repositoryName := fmt.Sprintf("%s-state/%s/state", repoType, repo)
	doubleEncodedRepoName := url.QueryEscape(url.QueryEscape(repositoryName))
	return fmt.Sprintf("%s/api/v2.0/projects/satellite/repositories/%s", registry, doubleEncodedRepoName)
}

func getEnvVar(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s environment variable is not set", key)
	}
	return value, nil
}

func stripProtocol(url string) string {
	if strings.Contains(url, "://") {
		return strings.SplitN(url, "://", 2)[1]
	}
	return url
}

func pushImage(img v1.Image, destination string, options []crane.Option) error {
	if err := crane.Push(img, destination, options...); err != nil {
		return fmt.Errorf("failed to push image: %v", err)
	}
	return nil
}

func tagImage(destination string, options []crane.Option) error {
	tags := []string{fmt.Sprintf("%d", time.Now().Unix()), "latest"}
	for _, tag := range tags {
		if err := crane.Tag(destination, tag, options...); err != nil {
			return fmt.Errorf("failed to tag image with %s: %v", tag, err)
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
	matched, _ := regexp.MatchString(pattern, name)
	return matched
}

func envSanityCheck() error {
	if registry == "" {
		return fmt.Errorf("HARBOR_URL environment variable is not set")
	}
	if username == "" {
		return fmt.Errorf("HARBOR_USERNAME environment variable is not set")
	}
	if password == "" {
		return fmt.Errorf("HARBOR_PASSWORD environment variable is not set")
	}
	return nil
}
