package satellite

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/notifier"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/state"
	"github.com/container-registry/harbor-satellite/internal/utils"
)

type Satellite struct {
	schedulerKey          scheduler.SchedulerKey
	LocalRegistryConfig   state.RegistryConfig
	SourcesRegistryConfig state.RegistryConfig
	UseUnsecure           bool
	state                 string
}

func NewSatellite(ctx context.Context, schedulerKey scheduler.SchedulerKey, localRegistryConfig, sourceRegistryConfig state.RegistryConfig, useUnsecure bool, state string) *Satellite {
	return &Satellite{
		schedulerKey:          schedulerKey,
		LocalRegistryConfig:   localRegistryConfig,
		SourcesRegistryConfig: sourceRegistryConfig,
		UseUnsecure:           useUnsecure,
		state:                 state,
	}
}

func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msg("Starting Satellite")
	replicateStateCron := config.GetStateReplicationInterval()
	updateConfigCron := config.GetUpdateConfigInterval()
	ztrCron := config.GetRegistrationInterval()
	// Get the scheduler from the context
	scheduler := ctx.Value(s.schedulerKey).(scheduler.Scheduler)
	// Create a simple notifier and add it to the process
	notifier := notifier.NewSimpleNotifier(ctx)
	// Creating a process to fetch and replicate the state
	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(replicateStateCron, notifier, s.SourcesRegistryConfig, s.LocalRegistryConfig, s.UseUnsecure, config.GetState())
	configFetchProcess := state.NewFetchConfigFromGroundControlProcess(updateConfigCron, "", "")
	ztrProcess := state.NewZtrProcess(ztrCron)
	err := scheduler.Schedule(configFetchProcess)
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

	// Schedule Register Satellite Process
	if utils.IsZTRDone() {
		log.Info().Msg("ZTR already performed, skipping the process")
		return nil
	}

	err = scheduler.Schedule(ztrProcess)
	if err != nil {
		log.Error().Err(err).Msg("Error scheduling process")
		return err
	}

	return nil
}
