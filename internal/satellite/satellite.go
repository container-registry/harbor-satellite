package satellite

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/notifier"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/state"
	"github.com/container-registry/harbor-satellite/internal/logger"
)

type RegistryConfig struct {
	URL      string
	UserName string
	Password string
}

func NewRegistryConfig(url, username, password string) RegistryConfig {
	return RegistryConfig{
		URL:      url,
		UserName: username,
		Password: password,
	}
}

type Satellite struct {
	schedulerKey          scheduler.SchedulerKey
	LocalRegistryConfig   RegistryConfig
	SourcesRegistryConfig RegistryConfig
	UseUnsecure           bool
	states                []string
}

func NewSatellite(ctx context.Context, schedulerKey scheduler.SchedulerKey, localRegistryConfig, sourceRegistryConfig RegistryConfig, useUnsecure bool, states []string) *Satellite {
	return &Satellite{
		schedulerKey:          schedulerKey,
		LocalRegistryConfig:   localRegistryConfig,
		SourcesRegistryConfig: sourceRegistryConfig,
		UseUnsecure:           useUnsecure,
		states:                states,
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
	states := config.GetStates()
	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(replicateStateCron, notifier, s.SourcesRegistryConfig.URL, s.SourcesRegistryConfig.UserName, s.SourcesRegistryConfig.Password, s.LocalRegistryConfig.URL, s.LocalRegistryConfig.UserName, s.LocalRegistryConfig.Password, s.UseUnsecure, states)
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
	err = scheduler.Schedule(ztrProcess)
	if err != nil {
		log.Error().Err(err).Msg("Error scheduling process")
		return err
	}
	return nil
}
