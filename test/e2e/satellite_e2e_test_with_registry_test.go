package e2e

import (
	"context"
	"os"
	"testing"

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

	s.satelliteTestSuite.postgres = startPostgres(s.satelliteTestSuite.ctx, s.satelliteTestSuite.client)
	require.NotNil(t, s.satelliteTestSuite.postgres, "postgresql service should not be nil")

	s.satelliteTestSuite.groundControl = startGroundControl(s.satelliteTestSuite.ctx, s.satelliteTestSuite.client)
	require.NotNil(t, s.satelliteTestSuite.groundControl, "ground control service should not be nil")

	checkHealthGroundControl(s.satelliteTestSuite.ctx, s.satelliteTestSuite.client, s.satelliteTestSuite.groundControl)

	s.harborRegistry.SetupHarborRegistry()
	t.Log("test environment setup complete")

}

func TestSatelliteWithRegistry(t *testing.T) {
	s := NewSatelliteTestWithRegistry(t)

	s.Setup(t)

}
