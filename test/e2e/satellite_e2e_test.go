package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"dagger.io/dagger"
	"github.com/container-registry/harbor-satellite/test/e2e/testconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type SatelliteTestSuite struct {
	client        *dagger.Client
	ctx           context.Context
	postgres      *dagger.Service
	groundControl *dagger.Service
	projectDir    string
}

func NewSatelliteTestSuite(t *testing.T) *SatelliteTestSuite {
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	require.NoError(t, err, "failed to connect to Dagger")

	projectDir, err := getProjectRootDir()
	require.NoError(t, err, "failed to get project directory")

	return &SatelliteTestSuite{
		client:     client,
		ctx:        ctx,
		projectDir: projectDir,
	}
}

func (s *SatelliteTestSuite) Clean(t *testing.T) {
	if err := s.client.Close(); err != nil {
		t.Logf("error closing client: %v", err)
	}
}

func (s *SatelliteTestSuite) Setup(t *testing.T) {
	t.Log("setting up test environment...")

	s.postgres = startPostgres(s.ctx, s.client)
	require.NotNil(t, s.postgres, "postgresql service should not be nil")

	s.groundControl = startGroundControl(s.ctx, s.client)
	require.NotNil(t, s.groundControl, "ground control service should not be nil")

	checkHealthGroundControl(s.ctx, s.client, s.groundControl)

	t.Log("test environment setup complete")
}

func TestSatelliteE2E(t *testing.T) {
	s := NewSatelliteTestSuite(t)

	s.Setup(t)

	t.Run("CreateConfigurationAndGroup", func(t *testing.T) {
		s.testCreateConfigurationAndGroup(t)
	})

	t.Run("RegisterSatelliteAndZTR", func(t *testing.T) {
		s.testRegisterSatelliteAndZTR(t)
	})

	s.Clean(t)
}

func (s *SatelliteTestSuite) testCreateConfigurationAndGroup(t *testing.T) {
	t.Log("Creating satellite configuration and group...")

	data, err := os.ReadFile(fmt.Sprintf("%s/test/e2e/testdata/new_config.json", s.projectDir))
	if err != nil {
		log.Fatalf("error reading file: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		require.NoError(t, err, "failed to unmarshal satellite config")
	}

	configReq := map[string]any{
		"config_name": "test-config",
		"config":      config,
	}

	_, err = s.makeGroundControlRequest(t, "POST", "/configs", configReq)
	require.NoError(t, err, "failed to create satellite configuration")

	groupReq := map[string]any{
		"group": "test-group",
		"artifacts": []map[string]any{
			{
				"repository": "nova/e2e",
				"tags":       []string{"latest"},
				"type":       "docker",
				"digest":     "sha256:cac266caf4e263fe8443dad75e4705152ff7dec09a7ed7b65b1b8c73a96bb1d6",
				"deleted":    false,
			},
		},
	}

	_, err = s.makeGroundControlRequest(t, "POST", "/groups/sync", groupReq)
	require.NoError(t, err, "failed to create group")

	t.Log("configuration and group created successfully")
}

func (s *SatelliteTestSuite) testRegisterSatelliteAndZTR(t *testing.T) {
	//TODO:// we need clean up robot account after each test
	name := generateUniqueSatelliteName("test-satellite")

	registerReq := map[string]any{
		"name":        name,
		"groups":      []string{"test-group"},
		"config_name": "test-config",
	}

	registerResp, err := s.makeGroundControlRequest(t, "POST", "/satellites", registerReq)
	require.NoError(t, err, "failed to register satellite")

	var resp map[string]any
	if err := json.Unmarshal([]byte(registerResp), &resp); err != nil {
		fmt.Println("error unmarshalling JSON:", err)
		return
	}

	token, exists := resp["token"]
	require.True(t, exists, "response should contain token")
	require.NotEmpty(t, token, "token should not be empty")

	t.Logf("satellite registered successfully with token: %v", token)

	s.pushImageToSourceRegistry(t)

	//ZTR
	configPath := fmt.Sprintf("%s/test/e2e/testdata/new_config.json", s.projectDir)

	satelliteDir := s.client.Host().Directory(s.projectDir)

	_, _ = s.client.Container().
		From("golang:1.24-alpine@sha256:68932fa6d4d4059845c8f40ad7e654e626f3ebd3706eef7846f319293ab5cb7a").
		WithMountedCache("/go/pkg/mod", s.client.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", s.client.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithDirectory("/app", satelliteDir).
		WithWorkdir("/app").
		WithServiceBinding("gc", s.groundControl).
		WithFile("/app/config.json", s.client.Host().File(configPath)).
		WithEnvVariable("TOKEN", strings.Trim(token.(string), `"`)).
		WithEnvVariable("GROUND_CONTROL_URL", "http://gc:8080").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"go", "build", "-o", "satellite", "cmd/main.go"}).
		WithExec([]string{"./satellite"}).
		Stdout(s.ctx)

	//TODO: check err and stdout to see if satellite started successfully
	t.Log("Satellite startup and ZTR process completed successfully")

}

func (s *SatelliteTestSuite) pushImageToSourceRegistry(t *testing.T) {
	t.Log("Setting up test images in source registry...")

	imageRef := "demo.goharbor.io/nova/e2e/alpine-test:latest"
	secret := s.client.SetSecret(testconfig.EnvHarborPassword, cfg.HarborPassword)

	stdout, err := s.client.Container().
		From("alpine@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715").
		WithRegistryAuth("demo.goharbor.io", "nova", secret).
		Publish(s.ctx, imageRef)

	assert.NoError(t, err, "failed to push image to source registry")

	t.Logf("Image URL: %s", stdout)

	t.Log("Test images setup completed")
}

func (s *SatelliteTestSuite) makeGroundControlRequest(t *testing.T, method, path string, body any) (string, error) {
	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	httpContainer := s.client.Container().
		From("alpine@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715").
		WithServiceBinding("gc", s.groundControl).
		WithExec([]string{"apk", "add", "curl"}).
		WithEnvVariable("CACHEBUSTER", time.Now().String())

	var curlArgs []string
	curlArgs = append(curlArgs, "curl", "-sX", method)

	if body != nil {
		curlArgs = append(curlArgs, "-H", "Content-Type: application/json")
		curlArgs = append(curlArgs, "-d", string(reqBody))
	}

	curlArgs = append(curlArgs, fmt.Sprintf("http://gc:8080%s", path))

	stdout, err := httpContainer.WithExec(curlArgs).Stdout(s.ctx)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}

	t.Logf("ground control api %s %s response: %s", method, path, stdout)

	return stdout, nil
}

func startPostgres(ctx context.Context, client *dagger.Client) *dagger.Service {
	db, err := client.Container().
		From("postgres:17@sha256:6cf6142afacfa89fb28b894d6391c7dcbf6523c33178bdc33e782b3b533a9342").
		WithEnvVariable("POSTGRES_USER", "postgres").
		WithEnvVariable("POSTGRES_PASSWORD", "password").
		WithEnvVariable("POSTGRES_DB", "groundcontrol").
		WithExposedPort(5432).
		AsService().WithHostname("postgres").Start(ctx)

	if err != nil {
		log.Fatalf("Failed to start PostgreSQL service: %v", err)
	}
	return db
}

func startGroundControl(ctx context.Context, client *dagger.Client) *dagger.Service {
	projectRoot, err := getProjectRootDir()
	if err != nil {
		log.Fatalf("failed to get project root dir %v", err)
	}

	gcDir := client.Host().Directory(fmt.Sprintf("%s/ground-control", projectRoot))

	gc, err := client.Container().
		From("golang:1.24-alpine@sha256:68932fa6d4d4059845c8f40ad7e654e626f3ebd3706eef7846f319293ab5cb7a").
		WithMountedCache("/go/pkg/mod", client.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", client.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithDirectory("/app", gcDir).
		WithWorkdir("/app").
		WithEnvVariable("DB_HOST", "postgres").
		WithEnvVariable("DB_PORT", "5432").
		WithEnvVariable("DB_USERNAME", "postgres").
		WithEnvVariable("DB_PASSWORD", "password").
		WithEnvVariable("DB_DATABASE", "groundcontrol").
		WithEnvVariable("PORT", "8080").
		WithEnvVariable(testconfig.EnvHarborUsername, cfg.HarborUsername).
		WithEnvVariable(testconfig.EnvHarborPassword, cfg.HarborPassword).
		WithEnvVariable("HARBOR_URL", "http://core:8080").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"go", "install", "github.com/pressly/goose/v3/cmd/goose@latest"}).
		WithWorkdir("/app/sql/schema").
		WithExec([]string{"goose", "postgres",
			"postgres://postgres:password@postgres:5432/groundcontrol?sslmode=disable", "up"}).
		WithWorkdir("/app").
		WithExec([]string{"go", "build", "-o", "ground-control", "main.go"}).
		WithExposedPort(8080, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithEntrypoint([]string{"./ground-control"}).
		AsService().WithHostname("gc").Start(ctx)

	if err != nil {
		log.Fatalf("failed to start ground control service: %v", err)
	}
	return gc
}

func checkHealthGroundControl(ctx context.Context, client *dagger.Client, gc *dagger.Service) string {
	container := client.Container().
		From("alpine@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithServiceBinding("gc", gc).
		WithExec([]string{"wget", "-qO-", "http://gc:8080/ping"})

	out, err := container.Stdout(ctx)
	if err != nil {
		log.Fatalf("health check failed for ground control service: %v", err)
	}

	return out
}
