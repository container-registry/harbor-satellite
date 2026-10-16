package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestApplyRotatedCredentials_PersistsAndReportsGeneration(t *testing.T) {
	pending.Plan(t, "8.4", "robots are not rotated; the satellite refreshes its secret in the request (D2)")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cm, err := config.NewConfigManager(path, filepath.Join(dir, "prev.json"), "token", "http://gc", false, &config.Config{
		StateConfig: config.StateConfig{
			RegistryCredentials: config.RegistryCredentials{URL: "http://harbor.example", Username: "robot$edge-01-7", Password: "old"},
			StateURL:            "http://harbor.example/satellite/satellite-state/edge-01/state:latest",
		},
		AppConfig: config.AppConfig{GroundControlURL: "http://gc", UseUnsecure: true},
	})
	require.NoError(t, err)

	require.NoError(t, ApplyRotatedCredentials(cm, gcclient.RotatedCredentials{
		Username: "robot$edge-01-7-g1", Password: "successor", ExpiresAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), Generation: 1,
	}))

	state := cm.GetStateConfig()
	require.Equal(t, "robot$edge-01-7-g1", state.RegistryCredentials.Username)
	require.Equal(t, "successor", state.RegistryCredentials.Password)
	require.Equal(t, "http://harbor.example", string(state.RegistryCredentials.URL), "the registry stays")
	require.Equal(t, int64(1), state.CredentialsGeneration)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var onDisk config.Config
	require.NoError(t, json.Unmarshal(raw, &onDisk))
	require.Equal(t, int64(1), onDisk.StateConfig.CredentialsGeneration, "written to config.json, so a restart reports it")
}
