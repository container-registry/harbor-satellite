package satellite

import (
	"context"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/notifier"
	"container-registry.com/harbor-satellite/internal/scheduler"
	"container-registry.com/harbor-satellite/internal/state"
	"container-registry.com/harbor-satellite/logger"
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
	replicateStateCron, err := config.GetJobSchedule(config.ReplicateStateJobName)
	if err != nil {
		log.Error().Err(err).Msg("Error getting schedule")
		return err
	}
	updateConfigCron, err := config.GetJobSchedule(config.UpdateConfigJobName)
	if err != nil {
		log.Error().Err(err).Msg("Error getting schedule")
		return err
	}
	ztrCron, err := config.GetJobSchedule(config.ZTRConfigJobName)
	if err != nil {
		log.Error().Err(err).Msg("Error getting schedule")
		return err
	}
	// Get the scheduler from the context
	scheduler := ctx.Value(s.schedulerKey).(scheduler.Scheduler)
	// Create a simple notifier and add it to the process
	notifier := notifier.NewSimpleNotifier(ctx)
	// Creating a process to fetch and replicate the state
	states := config.GetStates()
	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(replicateStateCron, notifier, s.SourcesRegistryConfig.URL, s.SourcesRegistryConfig.UserName, s.SourcesRegistryConfig.Password, s.LocalRegistryConfig.URL, s.LocalRegistryConfig.UserName, s.LocalRegistryConfig.Password, s.UseUnsecure, states)
	configFetchProcess := state.NewFetchConfigFromGroundControlProcess(updateConfigCron, "", "")
	ztrProcess := state.NewZtrProcess(ztrCron)
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
	// Schedule Register Satellite Process
	err = scheduler.Schedule(ztrProcess)
	if err != nil {
		log.Error().Err(err).Msg("Error scheduling process")
		return err
	}
	return nil
}
