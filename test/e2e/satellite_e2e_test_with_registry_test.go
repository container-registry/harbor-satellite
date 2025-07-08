package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"dagger.io/dagger"
	"github.com/stretchr/testify/require"
)

type SatelliteTestWithRegistrySuite struct {
	harborRegistry     *HarborRegistry
	satelliteTestSuite *SatelliteTestSuite
}

func NewSatelliteTestWithRegistry(t *testing.T) *SatelliteTestWithRegistrySuite {
	ctx := context.Background()

	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	require.NoError(t, err, "failed to connect to Dagger")

	projectDir, err := getProjectRootDir()
	require.NoError(t, err, "failed to get project directory")

	hr, err := NewHarborRegistry(ctx, client, projectDir)
	require.NoError(t, err, "failed to create harbor registry")

	return &SatelliteTestWithRegistrySuite{
		harborRegistry: hr,
		satelliteTestSuite: &SatelliteTestSuite{
			client:     client,
			ctx:        ctx,
			projectDir: projectDir,
		},
	}
}

func (s *SatelliteTestWithRegistrySuite) Setup(t *testing.T) {
	t.Log("setting up test environment...")

	// s.satelliteTestSuite.postgres = startPostgres(s.satelliteTestSuite.ctx, s.satelliteTestSuite.client)
	// require.NotNil(t, s.satelliteTestSuite.postgres, "postgresql service should not be nil")
	//
	// s.satelliteTestSuite.groundControl = startGroundControl(s.satelliteTestSuite.ctx, s.satelliteTestSuite.client)
	// require.NotNil(t, s.satelliteTestSuite.groundControl, "ground control service should not be nil")

	// checkHealthGroundControl(s.satelliteTestSuite.ctx, s.satelliteTestSuite.client, s.satelliteTestSuite.groundControl)

	s.harborRegistry.SetupHarborRegistry(t)
	t.Log("test environment setup complete")

}

func TestSatelliteWithRegistry(t *testing.T) {
	s := NewSatelliteTestWithRegistry(t)

	s.Setup(t)

	t.Run("RegisterSatelliteAndZTR", func(t *testing.T) {
		s.testRegisterSatelliteAndZTR(t)
	})

}

func (s *SatelliteTestWithRegistrySuite) testRegisterSatelliteAndZTR(t *testing.T) {
	name := generateUniqueSatelliteName("test-satellite")

	registerReq := map[string]any{
		"name":        name,
		"groups":      []string{"test-group"},
		"config_name": "test-config",
	}

	registerResp, err := s.satelliteTestSuite.makeGroundControlRequest(t, "POST", "/satellites", registerReq)
	require.NoError(t, err, "failed to register satellite")

	var resp map[string]any
	if err := json.Unmarshal([]byte(registerResp), &resp); err != nil {
		require.NoError(t, err, "failed to unmarshal register satellite respone")
	}

	token, exists := resp["token"]
	require.True(t, exists, "response should contain token")
	require.NotEmpty(t, token, "token should not be empty")

	t.Logf("satellite registered successfully with token: %v", token)

	//ZTR
	configPath := fmt.Sprintf("%s/test/e2e/testdata/new_config.json", s.satelliteTestSuite.projectDir)

	satelliteDir := s.satelliteTestSuite.client.Host().Directory(s.satelliteTestSuite.projectDir)

	_, err = s.satelliteTestSuite.client.Container().
		From("golang:1.24-alpine@sha256:68932fa6d4d4059845c8f40ad7e654e626f3ebd3706eef7846f319293ab5cb7a").
		WithMountedCache("/go/pkg/mod", s.satelliteTestSuite.client.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", s.satelliteTestSuite.client.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithDirectory("/app", satelliteDir).
		WithWorkdir("/app").
		WithServiceBinding("gc", s.satelliteTestSuite.groundControl).
		WithFile("/app/config.json", s.satelliteTestSuite.client.Host().File(configPath)).
		WithEnvVariable("TOKEN", token.(string)).
		WithEnvVariable("GROUND_CONTROL_URL", "http://gc:8080").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"go", "build", "-o", "satellite", "cmd/main.go"}).
		WithExec([]string{"./satellite"}).
		Stdout(s.satelliteTestSuite.ctx)

	if err != nil {
		require.NoError(t, err, "failed to start satellite")
	}

	t.Log("Satellite startup and ZTR process completed successfully")
}
