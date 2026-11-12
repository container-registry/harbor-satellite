package hotreload

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type countingProcess struct{ runs atomic.Int64 }

func (p *countingProcess) Name() string                      { return "counting" }
func (p *countingProcess) Execute(ctx context.Context) error { p.runs.Add(1); return nil }
func (p *countingProcess) IsRunning() bool                   { return false }
func (p *countingProcess) IsComplete() bool                  { return false }

func writeConfig(t *testing.T, path, logLevel, interval string) {
	t.Helper()
	cfg := config.Config{AppConfig: config.AppConfig{LogLevel: logLevel, StateReplicationInterval: interval}}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

// newManager writes an hourly config and loads it through the same
// InitConfigManager and ReloadConfig calls cmd/satellite uses, so a change
// goes through validation first.
func newManager(t *testing.T) (*config.ConfigManager, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfig(t, path, "info", "@every 1h")
	cm, _, err := config.InitConfigManager("token", "http://127.0.0.1:8080", path, filepath.Join(dir, "prev.json"), false, false)
	require.NoError(t, err)
	return cm, path
}

func reload(t *testing.T, cm *config.ConfigManager) []config.ConfigChange {
	t.Helper()
	changes, _, err := cm.ReloadConfig()
	require.NoError(t, err)
	return changes
}

func TestIntervalChangeRestartsTheReplicationScheduler(t *testing.T) {
	log := zerolog.Nop()
	cm, path := newManager(t)
	proc := &countingProcess{}
	sched, err := scheduler.NewSchedulerWithInterval(cm.GetStateReplicationInterval(), proc, &log)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		require.NoError(t, sched.Stop(context.Background()))
	})
	sched.Start(ctx)
	require.Eventually(t, func() bool { return proc.runs.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	hrm := NewHotReloadManager(ctx, cm, &log, nil)
	hrm.SetStateReplicationScheduler(sched)
	writeConfig(t, path, "info", "@every 20ms")
	changes := reload(t, cm)
	require.Len(t, changes, 1)
	require.Equal(t, config.IntervalsChanged, changes[0].Type)
	require.NoError(t, hrm.ProcessConfigChanges(changes))

	require.Eventually(t, func() bool { return proc.runs.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"the scheduler still ticks at the old hourly interval")
}

func TestIntervalChangeWithoutSchedulerIsNoError(t *testing.T) {
	log := zerolog.Nop()
	cm, path := newManager(t)
	hrm := NewHotReloadManager(context.Background(), cm, &log, nil)
	writeConfig(t, path, "info", "@every 5m")
	require.NoError(t, hrm.ProcessConfigChanges(reload(t, cm)))
}

func TestIntervalChangeToANonEveryScheduleReportsTheError(t *testing.T) {
	log := zerolog.Nop()
	cm, path := newManager(t)
	sched, err := scheduler.NewSchedulerWithInterval("@every 1h", &countingProcess{}, &log)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sched.Stop(context.Background())) })
	hrm := NewHotReloadManager(context.Background(), cm, &log, sched)

	// A standard cron line passes config validation, but the scheduler
	// only understands @every.
	writeConfig(t, path, "info", "*/5 * * * *")
	err = hrm.ProcessConfigChanges(reload(t, cm))
	require.ErrorContains(t, err, "unable to restart state replication scheduler")
}

func TestLogLevelChangeSetsTheGlobalLevel(t *testing.T) {
	prev := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })
	log := zerolog.Nop()

	tests := []struct {
		name  string
		value any
		want  zerolog.Level
		err   string
	}{
		{name: "debug", value: "debug", want: zerolog.DebugLevel},
		{name: "error", value: "error", want: zerolog.ErrorLevel},
		{name: "unknown falls back to info", value: "loud", want: zerolog.InfoLevel},
		{name: "not a string", value: 3, want: zerolog.WarnLevel, err: "invalid log level type: int"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zerolog.SetGlobalLevel(zerolog.WarnLevel)
			cm, _ := newManager(t)
			hrm := NewHotReloadManager(context.Background(), cm, &log, nil)
			err := hrm.ProcessConfigChanges([]config.ConfigChange{{Type: config.LogLevelChanged, OldValue: "info", NewValue: tt.value}})
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want, zerolog.GlobalLevel())
		})
	}
}

func TestLogLevelReloadedFromDisk(t *testing.T) {
	prev := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log := zerolog.Nop()
	cm, path := newManager(t)
	hrm := NewHotReloadManager(context.Background(), cm, &log, nil)

	writeConfig(t, path, "warn", "@every 1h")
	require.NoError(t, hrm.ProcessConfigChanges(reload(t, cm)))
	require.Equal(t, zerolog.WarnLevel, zerolog.GlobalLevel())
}

func TestChangeWithoutCallbackIsIgnored(t *testing.T) {
	log := zerolog.Nop()
	cm, _ := newManager(t)
	hrm := NewHotReloadManager(context.Background(), cm, &log, nil)
	require.NoError(t, hrm.ProcessConfigChanges([]config.ConfigChange{{Type: config.AuditConfigChanged}}))
	require.NoError(t, hrm.ProcessConfigChanges(nil))
}

func TestErrorsOfEveryChangeAreReturned(t *testing.T) {
	log := zerolog.Nop()
	cm, _ := newManager(t)
	hrm := NewHotReloadManager(context.Background(), cm, &log, nil)
	err := hrm.ProcessConfigChanges([]config.ConfigChange{
		{Type: config.LogLevelChanged, NewValue: 1},
		{Type: config.LogLevelChanged, NewValue: true},
	})
	require.ErrorContains(t, err, "int")
	require.ErrorContains(t, err, "bool")
}

func TestIntervalChangeToZeroDoesNotPanic(t *testing.T) {
	log := zerolog.Nop()
	cm, path := newManager(t)
	sched, err := scheduler.NewSchedulerWithInterval("@every 1h", &countingProcess{}, &log)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sched.Stop(context.Background())) })
	hrm := NewHotReloadManager(context.Background(), cm, &log, sched)

	for _, interval := range []string{"@every 0s", "@every -5s"} {
		writeConfig(t, path, "info", interval)
		changes, warnings, err := cm.ReloadConfig()
		require.NoError(t, err)
		require.Len(t, warnings, 1, interval)
		require.Equal(t, config.DefaultFetchAndReplicateCronExpr, cm.GetStateReplicationInterval())
		require.NotPanics(t, func() { require.NoError(t, hrm.ProcessConfigChanges(changes)) }, interval)
	}
}

// A config from Ground Control usually changes heartbeat_interval after the
// satellite started with the default; the satellite reports the configured
// interval on every beat, so it must also beat at that interval.
func TestHeartbeatIntervalChangeRestartsTheStatusScheduler(t *testing.T) {
	log := zerolog.Nop()
	cm, path := newManager(t)
	proc := &countingProcess{}
	sched, err := scheduler.NewSchedulerWithInterval(cm.GetHeartbeatInterval(), proc, &log)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		require.NoError(t, sched.Stop(context.Background()))
	})
	sched.Start(ctx)
	require.Eventually(t, func() bool { return proc.runs.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	hrm := NewHotReloadManager(ctx, cm, &log, nil)
	hrm.SetStatusReportScheduler(sched)
	cfg := config.Config{AppConfig: config.AppConfig{LogLevel: "info", StateReplicationInterval: "@every 1h", HeartbeatInterval: "@every 20ms"}}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	changes := reload(t, cm)
	require.Len(t, changes, 1)
	require.Equal(t, config.HeartbeatIntervalChanged, changes[0].Type)
	require.NoError(t, hrm.ProcessConfigChanges(changes))

	require.Eventually(t, func() bool { return proc.runs.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"the status report scheduler still ticks at the startup interval")
}

func TestHeartbeatIntervalChangeWithoutSchedulerIsNoError(t *testing.T) {
	log := zerolog.Nop()
	cm, _ := newManager(t)
	hrm := NewHotReloadManager(context.Background(), cm, &log, nil)
	require.NoError(t, hrm.ProcessConfigChanges([]config.ConfigChange{{Type: config.HeartbeatIntervalChanged}}))
}
