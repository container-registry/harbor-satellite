package satellite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func newTestConfigManager(t *testing.T) *config.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	prevPath := filepath.Join(dir, "prev_config.json")

	cfg := &config.Config{
		AppConfig: config.AppConfig{
			GroundControlURL:          "http://gc:8080",
			StateReplicationInterval:  "@every 1m",
			HeartbeatInterval:         "@every 30s",
			RegisterSatelliteInterval: "@every 1m",
			LocalRegistryCredentials:  config.RegistryCredentials{URL: "http://localhost:8585"},
		},
		StateConfig: config.StateConfig{
			StateURL:            "http://gc:8080/state",
			RegistryCredentials: config.RegistryCredentials{URL: "http://harbor:8080"},
		},
		ZotConfigRaw: json.RawMessage(`{"http":{"address":"0.0.0.0","port":"8585"}}`),
	}

	cm, err := config.NewConfigManager(configPath, prevPath, "test-token", "http://gc:8080", false, cfg)
	require.NoError(t, err)
	return cm
}

func TestNewSatellite(t *testing.T) {
	tests := []struct {
		name          string
		stateFilePath string
	}{
		{
			name:          "with state file path",
			stateFilePath: "/tmp/state.json",
		},
		{
			name:          "with empty state file path",
			stateFilePath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := newTestConfigManager(t)

			satellite := NewSatellite(cm, tt.stateFilePath)

			require.NotNil(t, satellite)
			require.Equal(t, cm, satellite.cm)
			require.Equal(t, tt.stateFilePath, satellite.stateFilePath)
			require.NotNil(t, satellite.schedulers)
			require.Empty(t, satellite.schedulers)
		})
	}
}

func TestSatelliteRun_WithZTR(t *testing.T) {
	cm := newTestConfigManager(t)
	stateFilePath := filepath.Join(t.TempDir(), "state.json")

	satellite := NewSatellite(cm, stateFilePath)
	require.NotNil(t, satellite)

	// Create a context that's already cancelled to avoid long test runs
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Initialize logger in context
	ctx, _ = logger.InitLogger(ctx, "info", false, nil)

	// Run satellite - should return quickly with cancelled context
	err := satellite.Run(ctx)
	// May return context.Canceled or nil depending on timing
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}

	// Verify schedulers were created
	schedulers := satellite.GetSchedulers()
	require.NotNil(t, schedulers)
	// Schedulers should be created even with cancelled context
	require.GreaterOrEqual(t, len(schedulers), 2)
}

func TestSatelliteRun_SchedulerCreation(t *testing.T) {
	cm := newTestConfigManager(t)
	stateFilePath := filepath.Join(t.TempDir(), "state.json")

	satellite := NewSatellite(cm, stateFilePath)
	require.NotNil(t, satellite)

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Initialize logger in context
	ctx, _ = logger.InitLogger(ctx, "info", false, nil)

	// Run satellite - should return quickly with cancelled context
	err := satellite.Run(ctx)
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}

	// Verify schedulers were created
	schedulers := satellite.GetSchedulers()
	require.NotNil(t, schedulers)
	// Should have multiple schedulers (ZTR, state replication, status report)
	require.GreaterOrEqual(t, len(schedulers), 2)
}

func TestSatelliteRun_WithSPIFFE(t *testing.T) {
	cm := newTestConfigManager(t)
	stateFilePath := filepath.Join(t.TempDir(), "state.json")

	// Enable SPIFFE
	cm.With(config.SetSPIFFEConfig(config.SPIFFEConfig{
		Enabled:          true,
		EndpointSocket:   "/tmp/spiffe.sock",
		ExpectedServerID: "spiffe://example.com/server",
	}))

	satellite := NewSatellite(cm, stateFilePath)
	require.NotNil(t, satellite)

	// Create a context that's already cancelled to test code path quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Initialize logger in context
	ctx, _ = logger.InitLogger(ctx, "info", false, nil)

	// Run satellite - should return quickly with cancelled context
	// SPIFFE process will be created but schedulers won't run long
	err := satellite.Run(ctx)
	// May return context.Canceled or nil depending on timing
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}

	// Verify schedulers were created
	schedulers := satellite.GetSchedulers()
	require.NotNil(t, schedulers)
	require.GreaterOrEqual(t, len(schedulers), 2)
}

func TestGetSchedulers(t *testing.T) {
	cm := newTestConfigManager(t)
	stateFilePath := filepath.Join(t.TempDir(), "state.json")

	satellite := NewSatellite(cm, stateFilePath)
	require.NotNil(t, satellite)

	// Initially should be empty
	schedulers := satellite.GetSchedulers()
	require.NotNil(t, schedulers)
	require.Empty(t, schedulers)

	// After running (with immediate cancellation), schedulers should be populated
	ctx, cancel := context.WithCancel(context.Background())
	ctx, _ = logger.InitLogger(ctx, "info", false, nil)

	// Cancel immediately to avoid blocking
	cancel()

	// Run will return quickly due to cancelled context
	_ = satellite.Run(ctx)

	// GetSchedulers should now return the created schedulers
	schedulers = satellite.GetSchedulers()
	require.NotNil(t, schedulers)
	// Even with cancelled context, schedulers should be created
	require.NotEmpty(t, schedulers)
}

func TestStop(t *testing.T) {
	cm := newTestConfigManager(t)
	stateFilePath := filepath.Join(t.TempDir(), "state.json")

	satellite := NewSatellite(cm, stateFilePath)
	require.NotNil(t, satellite)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ctx, _ = logger.InitLogger(ctx, "info", false, nil)

	// Run satellite in goroutine
	go func() {
		_ = satellite.Run(ctx)
	}()

	// Give it a moment to start schedulers
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic even if schedulers are running
	require.NotPanics(t, func() {
		satellite.Stop()
	})

	// Verify schedulers still exist
	schedulers := satellite.GetSchedulers()
	require.NotNil(t, schedulers)
}

func TestStop_EmptySchedulers(t *testing.T) {
	cm := newTestConfigManager(t)
	stateFilePath := filepath.Join(t.TempDir(), "state.json")

	satellite := NewSatellite(cm, stateFilePath)
	require.NotNil(t, satellite)

	// Stop should not panic even with empty schedulers
	require.NotPanics(t, func() {
		satellite.Stop()
	})
}

func TestSatelliteRun_InvalidIntervals(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	prevPath := filepath.Join(dir, "prev_config.json")

	// Create config with invalid interval format
	cfg := &config.Config{
		AppConfig: config.AppConfig{
			GroundControlURL:          "http://gc:8080",
			StateReplicationInterval:  "invalid",
			HeartbeatInterval:         "@every 30s",
			RegisterSatelliteInterval: "@every 1m",
			LocalRegistryCredentials:  config.RegistryCredentials{URL: "http://localhost:8585"},
		},
		StateConfig: config.StateConfig{
			StateURL:            "http://gc:8080/state",
			RegistryCredentials: config.RegistryCredentials{URL: "http://harbor:8080"},
		},
		ZotConfigRaw: json.RawMessage(`{"http":{"address":"0.0.0.0","port":"8585"}}`),
	}

	cm, err := config.NewConfigManager(configPath, prevPath, "test-token", "http://gc:8080", false, cfg)
	require.NoError(t, err)

	stateFilePath := filepath.Join(t.TempDir(), "state.json")
	satellite := NewSatellite(cm, stateFilePath)

	ctx, _ := logger.InitLogger(context.Background(), "info", false, nil)

	// Run should fail due to invalid interval
	err = satellite.Run(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse interval")
}

func TestSatelliteRun_NilConfigManager(t *testing.T) {
	stateFilePath := filepath.Join(t.TempDir(), "state.json")

	// This test verifies behavior with nil config manager
	satellite := &Satellite{
		cm:            nil,
		schedulers:    make([]*scheduler.Scheduler, 0),
		stateFilePath: stateFilePath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ctx, _ = logger.InitLogger(ctx, "info", false, nil)

	// Should panic or error with nil config manager
	require.Panics(t, func() {
		_ = satellite.Run(ctx)
	})
}