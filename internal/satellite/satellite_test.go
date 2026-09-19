package satellite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func satelliteTestContext() context.Context {
	log := zerolog.Nop()
	return context.WithValue(context.Background(), logger.LoggerKey, &log)
}

func satelliteTestConfigManager(t *testing.T, cfg *config.Config, token, groundControlURL string) *config.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev_config.json"),
		token,
		groundControlURL,
		false,
		cfg,
	)
	require.NoError(t, err)
	return cm
}

func TestRunWithoutGroundControlOnlySchedulesStateReplication(t *testing.T) {
	cfg := &config.Config{
		StateConfig: config.StateConfig{
			RegistryCredentials: config.RegistryCredentials{
				URL:      "https://harbor.example.com",
				Username: "robot$satellite",
				Password: "secret",
			},
			StateURL: "https://harbor.example.com/satellite/state/example:latest",
		},
		AppConfig: config.AppConfig{
			StateReplicationInterval: config.DefaultFetchAndReplicateCronExpr,
			HeartbeatInterval:        config.DefaultHeartbeatCronExpr,
		},
	}
	cm := satelliteTestConfigManager(t, cfg, "", "")
	log := zerolog.Nop()
	sat := NewSatellite(cm, nil, "", t.TempDir(), events.NewEventScheduler(&log))
	ctx, cancel := context.WithCancel(satelliteTestContext())
	cancel()

	require.NoError(t, sat.Run(ctx))
	require.Len(t, sat.GetSchedulers(), 1)
	require.Equal(t, config.ReplicateStateJobName, sat.GetSchedulers()[0].Name())

	stopCtx, stopCancel := context.WithTimeout(satelliteTestContext(), time.Second)
	defer stopCancel()
	sat.Stop(stopCtx)
}

func TestRunWithGroundControlSchedulesRegistrationAndReporting(t *testing.T) {
	cfg := &config.Config{
		AppConfig: config.AppConfig{
			StateReplicationInterval:  config.DefaultFetchAndReplicateCronExpr,
			RegisterSatelliteInterval: config.DefaultZTRCronExpr,
			HeartbeatInterval:         config.DefaultHeartbeatCronExpr,
		},
	}
	cm := satelliteTestConfigManager(t, cfg, "bootstrap-token", "https://ground-control.example.com")
	log := zerolog.Nop()
	sat := NewSatellite(cm, nil, "", t.TempDir(), events.NewEventScheduler(&log))
	ctx, cancel := context.WithCancel(satelliteTestContext())
	cancel()

	require.NoError(t, sat.Run(ctx))
	names := make([]string, 0, len(sat.GetSchedulers()))
	for _, scheduler := range sat.GetSchedulers() {
		names = append(names, scheduler.Name())
	}
	require.ElementsMatch(t, []string{
		config.ZTRConfigJobName,
		config.ReplicateStateJobName,
		config.StatusReportJobName,
	}, names)

	stopCtx, stopCancel := context.WithTimeout(satelliteTestContext(), time.Second)
	defer stopCancel()
	sat.Stop(stopCtx)
}
