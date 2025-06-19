package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"dagger.io/dagger"
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
	assert.NoError(t, err, "failed to connect to Dagger")

	projectDir, err := getProjectDir()
	assert.NoError(t, err, "failed to get project directory")

	return &SatelliteTestSuite{
		client:     client,
		ctx:        ctx,
		projectDir: projectDir,
	}
}

func (s *SatelliteTestSuite) Clean(t *testing.T) {
	if err := s.client.Close(); err != nil {
		log.Printf("error closing client: %v", err)
	}

}
func (s *SatelliteTestSuite) Setup(t *testing.T) {
	t.Log("setting up test environment...")

	s.postgres = startPostgres(s.ctx, s.client)
	require.NotNil(t, s.postgres, "postgresql service should not be nil")

	s.groundControl = startGroundControl(s.ctx, s.client, s.postgres)
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

	s.Clean(t)
}

func (s *SatelliteTestSuite) testCreateConfigurationAndGroup(t *testing.T) {
	t.Log("Creating satellite configuration and group...")

	data, err := os.ReadFile(fmt.Sprintf("%s/test/e2e/testdata/new_config.json", s.projectDir))
	if err != nil {
		log.Fatalf("error reading file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		fmt.Println("error unmarshalling JSON:", err)
		return
	}

	configReq := map[string]interface{}{
		"config_name": "test-config",
		"config":      config,
	}

	err = s.makeGroundControlRequest(t, "POST", "/configs/sync", configReq, nil)
	assert.NoError(t, err, "Failed to create satellite configuration")

	//TODO:// create group
	t.Log("Configuration created successfully")
}

func (s *SatelliteTestSuite) makeGroundControlRequest(t *testing.T, method, path string, body interface{}, response interface{}) error {
	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	httpContainer := s.client.Container().
		From("alpine:latest").
		WithServiceBinding("gc", s.groundControl).
		WithExec([]string{"apk", "add", "curl"})

	var curlArgs []string
	curlArgs = append(curlArgs, "curl", "-s", "-X", method)

	if body != nil {
		curlArgs = append(curlArgs, "-H", "Content-Type: application/json")
		curlArgs = append(curlArgs, "-d", string(reqBody))
	}

	curlArgs = append(curlArgs, fmt.Sprintf("http://gc:8080%s", path))

	stdout, err := httpContainer.WithExec(curlArgs).Stdout(s.ctx)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	t.Logf("ground control api %s %s response: %s", method, path, stdout)

	if response != nil && stdout != "" {
		if err := json.Unmarshal([]byte(stdout), response); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func startPostgres(ctx context.Context, client *dagger.Client) *dagger.Service {
	db, err := client.Container().
		From("postgres:17").
		WithEnvVariable("POSTGRES_USER", "postgres").
		WithEnvVariable("POSTGRES_PASSWORD", "password").
		WithEnvVariable("POSTGRES_DB", "groundcontrol").
		//WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExposedPort(5432).
		AsService().Start(ctx)

	if err != nil {
		log.Fatalf("Failed to start PostgreSQL service: %v", err)
	}
	return db
}

func startGroundControl(ctx context.Context, client *dagger.Client, db *dagger.Service) *dagger.Service {
	projectRoot, err := getProjectRoot()
	gcDir := client.Host().Directory(fmt.Sprintf("%s/ground-control", projectRoot))

	gc, err := client.Container().
		From("golang:latest").
		WithMountedCache("/go/pkg/mod", client.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", client.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithDirectory("/app", gcDir).
		WithWorkdir("/app").
		WithServiceBinding("postgres", db).
		WithEnvVariable("DB_HOST", "postgres").
		WithEnvVariable("DB_PORT", "5432").
		WithEnvVariable("DB_USERNAME", "postgres").
		WithEnvVariable("DB_PASSWORD", "password").
		WithEnvVariable("DB_DATABASE", "groundcontrol").
		WithEnvVariable("PORT", "8080").
		WithEnvVariable("HARBOR_URL", "http://172.22.0.2:30002").
		//	WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"go", "install", "github.com/pressly/goose/v3/cmd/goose@latest"}).
		WithWorkdir("/app/sql/schema").
		WithExec([]string{"goose", "postgres",
			"postgres://postgres:password@postgres:5432/groundcontrol?sslmode=disable", "up"}).
		WithWorkdir("/app").
		WithExec([]string{"go", "build", "-o", "ground-control", "main.go"}).
		WithExposedPort(8080).
		WithExec([]string{"./ground-control"}).
		AsService().Start(ctx)

	if err != nil {
		log.Fatalf("failed to start ground control service: %v", err)
	}
	return gc
}

func checkHealthGroundControl(ctx context.Context, client *dagger.Client, gc *dagger.Service) string {
	container := client.Container().
		From("alpine").
		WithServiceBinding("gc", gc).
		WithExec([]string{"wget", "-qO-", "http://gc:8080/ping"})

	out, err := container.Stdout(ctx)
	if err != nil {
		log.Fatalf("health check failed for ground control service: %v", err)
	}

	return out
}

func getProjectRoot() (string, error) {
	if projectRoot := os.Getenv("PROJECT_ROOT"); projectRoot != "" {
		return projectRoot, nil
	}

	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return filepath.Abs(filepath.Join(currentDir, "../.."))
}
