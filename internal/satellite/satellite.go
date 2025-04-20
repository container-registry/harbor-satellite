package satellite

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/notifier"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/state"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

type Satellite struct {
	schedulerKey scheduler.SchedulerKey
	cm           *config.ConfigManager
}

func NewSatellite(schedulerKey scheduler.SchedulerKey, cm *config.ConfigManager) *Satellite {
	return &Satellite{
		schedulerKey: schedulerKey,
		cm:           cm,
	}
}

func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msg("Starting Satellite")
	// Get the scheduler from the context
	scheduler := ctx.Value(s.schedulerKey).(scheduler.Scheduler)

	// Create a simple notifier and add it to the process
	notifier := notifier.NewSimpleNotifier(ctx)

	// Creating a process to fetch and replicate the state
	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(s.cm, notifier)
	configFetchProcess := state.NewFetchConfigFromGroundControlProcess(s.cm.GetUpdateConfigInterval(), "", "")
	ztrProcess := state.NewZtrProcess(s.cm.GetRegistrationInterval())

	// Schedule Register Satellite Process
	if s.cm.IsZTRDone() {
		log.Info().Msg("ZTR already performed, skipping the process")
		return nil
	}

	err := scheduler.Schedule(ztrProcess)
	if err != nil {
		log.Error().Err(err).Msg("Error scheduling process")
		return err
	}

	err = scheduler.Schedule(configFetchProcess)
	if err != nil {
		log.Error().Err(err).Msg("Error scheduling process")
		return err
	}

	// Add the process to the scheduler
	err = scheduler.Schedule(fetchAndReplicateStateProcess)
	if err != nil {
		log.Error().Err(err).Msg("Error scheduling process")
		return err
	}

	return nil
}
