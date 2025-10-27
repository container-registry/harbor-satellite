package hotreload

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/registry"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	cfg "zotregistry.dev/zot/pkg/api/config"
	"zotregistry.dev/zot/pkg/cli/server"
)

type HotReloadManager struct {
	cm                        *config.ConfigManager
	log                       *zerolog.Logger
	ctx                       context.Context
	stateReplicationScheduler *scheduler.Scheduler
	heartbeatScheduler        *scheduler.Scheduler
	changeCallbacks           map[config.ConfigChangeType][]config.ConfigChangeCallback
	callbackMu                sync.RWMutex
}

func NewHotReloadManager(
	ctx context.Context,
	cm *config.ConfigManager,
	log *zerolog.Logger,
	stateReplicationScheduler *scheduler.Scheduler,
	heartbeatScheduler *scheduler.Scheduler,
) *HotReloadManager {
	manager := &HotReloadManager{
		cm:                        cm,
		log:                       log,
		ctx:                       ctx,
		stateReplicationScheduler: stateReplicationScheduler,
		heartbeatScheduler:        heartbeatScheduler,
		changeCallbacks:           make(map[config.ConfigChangeType][]config.ConfigChangeCallback),
	}

	manager.registerCallbacks()

	return manager
}

func (hrm *HotReloadManager) registerCallbacks() {
	hrm.registerChangeCallback(config.IntervalsChanged, hrm.handleIntervalsChange)
	hrm.registerChangeCallback(config.ZotConfigChanged, hrm.handleZotConfigChange)
	hrm.registerChangeCallback(config.LogLevelChanged, hrm.handleLogLevelChange)
}

func (hrm *HotReloadManager) notifyChangeCallbacks(change config.ConfigChange) []error {
	hrm.callbackMu.RLock()
	defer hrm.callbackMu.RUnlock()

	callbacks, exists := hrm.changeCallbacks[change.Type]
	if !exists {
		return nil
	}

	var errors []error
	for _, callback := range callbacks {
		if err := callback(change); err != nil {
			errors = append(errors, err)
		}
		hrm.log.Info().Str("change_type", string(change.Type)).Msg("Configuration change processed")
	}

	return errors
}

func (hrm *HotReloadManager) registerChangeCallback(changeType config.ConfigChangeType, callback config.ConfigChangeCallback) {
	hrm.callbackMu.Lock()
	defer hrm.callbackMu.Unlock()

	if hrm.changeCallbacks[changeType] == nil {
		hrm.changeCallbacks[changeType] = make([]config.ConfigChangeCallback, 0)
	}

	hrm.changeCallbacks[changeType] = append(hrm.changeCallbacks[changeType], callback)
}

func (hrm *HotReloadManager) handleIntervalsChange(change config.ConfigChange) error {
	hrm.log.Info().
		Str("type", string(change.Type)).
		Interface("old_value", change.OldValue).
		Interface("new_value", change.NewValue).
		Msg("Handling intervals change")

	if hrm.stateReplicationScheduler != nil {
		hrm.log.Info().Msg("Restarting state replication scheduler with new interval")
		err := hrm.stateReplicationScheduler.ResetIntervalFromExpr(hrm.cm.GetStateReplicationInterval())
		if err != nil {
			return fmt.Errorf("unable to restart state replication scheduler: %w", err)
		}
	}

	if hrm.heartbeatScheduler != nil {
		hrm.log.Info().Msg("Restarting heartbeat scheduler with new interval")
		err := hrm.heartbeatScheduler.ResetIntervalFromExpr(hrm.cm.GetHeartbeatInterval())
		if err != nil {
			return fmt.Errorf("unable to heartbeat scheduler: %w", err)
		}
	}

	return nil
}

func (hrm *HotReloadManager) handleLogLevelChange(change config.ConfigChange) error {
	hrm.log.Info().
		Str("type", string(change.Type)).
		Interface("old_value", change.OldValue).
		Interface("new_value", change.NewValue).
		Msg("Handling log level change")

	newLogLevel, ok := change.NewValue.(string)
	if !ok {
		return fmt.Errorf("invalid log level type: %T", change.NewValue)
	}
	level, err := zerolog.ParseLevel(newLogLevel)
	if err != nil {
		hrm.log.Error().
			Str("provided_level", newLogLevel).
			Msg("Unknown log level. Defaulting to 'info'")
		level = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(level)
	hrm.log.Info().Str("new_level", level.String()).Msg("Log level updated successfully")

	return nil
}

func (hrm *HotReloadManager) handleZotConfigChange(change config.ConfigChange) error {
	hrm.log.Info().
		Str("type", string(change.Type)).
		Msg("Handling Zot configuration change")

	hrm.log.Warn().
		Str("type", string(change.Type)).
		Msg("Some Zot configuration may require to restart")

	//verify the zot configuration before apply
	var cfg cfg.Config
	if err := json.Unmarshal(hrm.cm.GetRawZotConfig(), &cfg); err != nil {
		return fmt.Errorf("unable to unmarshal zot config: %w, defaulting to previous zot configuration", err)
	}

	//Zot verify the config using below function hence taking same the path
	if err := server.LoadConfiguration(&cfg, registry.ZotTempPath); err != nil {
		hrm.log.Error().Interface("config", &cfg).Msg("invalid config file")
		return err
	}

	err := os.WriteFile(registry.ZotTempPath, hrm.cm.GetRawZotConfig(), 0644)
	if err != nil {
		return fmt.Errorf("unable to change zot configuration: %w", err)
	}

	return nil
}
func (hrm *HotReloadManager) SetStateReplicationScheduler(stateReplicationScheduler *scheduler.Scheduler) {
	hrm.stateReplicationScheduler = stateReplicationScheduler
}

func (hrm *HotReloadManager) ProcessConfigChanges(changes []config.ConfigChange) error {

	hrm.log.Info().Int("change_count", len(changes)).Msg("Processing configuration changes")

	var errors []error

	for _, change := range changes {
		hrm.log.Debug().
			Str("change_type", string(change.Type)).
			Interface("old_value", change.OldValue).
			Interface("new_value", change.NewValue).
			Msg("Processing configuration change")

		errors = append(errors, hrm.notifyChangeCallbacks(change)...)
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors occurred while processing configuration changes: %v", errors)
	}

	hrm.log.Info().Msg("All configuration changes processed successfully")

	return nil
}
