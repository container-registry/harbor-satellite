package satellite

import (
	"context"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/notifier"
	"github.com/container-registry/harbor-satellite/internal/replicator"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/state"
	"github.com/container-registry/harbor-satellite/internal/transfer"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/rs/zerolog"
)

// Satellite represents the main satellite service
type Satellite struct {
	schedulerKey          scheduler.SchedulerKey
	LocalRegistryConfig   state.RegistryConfig
	SourcesRegistryConfig state.RegistryConfig
	UseUnsecure           bool
	state                 string
	config                *config.Config
	replicator            *replicator.Replicator
	meter                 *transfer.TransferMeter
	logger                zerolog.Logger
}

// NewSatellite creates a new satellite service
func NewSatellite(ctx context.Context, schedulerKey scheduler.SchedulerKey, localRegistryConfig, sourceRegistryConfig state.RegistryConfig, useUnsecure bool, state string, cfg *config.Config) (*Satellite, error) {
	log := logger.FromContext(ctx)

	// Create transfer meter with limits from config
	meter := transfer.NewTransferMeter(
		cfg.TransferLimits.HourlyLimit,
		cfg.TransferLimits.DailyLimit,
		cfg.TransferLimits.WeeklyLimit,
		cfg.TransferLimits.MonthlyLimit,
	)

	// Create replicator
	rep, err := replicator.NewReplicator(cfg, meter)
	if err != nil {
		return nil, fmt.Errorf("failed to create replicator: %w", err)
	}

	return &Satellite{
		schedulerKey:          schedulerKey,
		LocalRegistryConfig:   localRegistryConfig,
		SourcesRegistryConfig: sourceRegistryConfig,
		UseUnsecure:           useUnsecure,
		state:                 state,
		config:                cfg,
		replicator:            rep,
		meter:                 meter,
		logger:                log.With().Str("component", "satellite").Logger(),
	}, nil
}

// Start begins the satellite service
func (s *Satellite) Start(ctx context.Context) error {
	s.logger.Info().Msg("starting satellite service")
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
		s.logger.Error().Err(err).Msg("Error scheduling process")
		return err
	}
	// Add the process to the scheduler
	err = scheduler.Schedule(fetchAndReplicateStateProcess)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error scheduling process")
		return err
	}

	// Schedule Register Satellite Process
	if utils.IsZTRDone() {
		s.logger.Info().Msg("ZTR already performed, skipping the process")
		return nil
	}

	err = scheduler.Schedule(ztrProcess)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error scheduling process")
		return err
	}

	return nil
}

// Stop gracefully shuts down the satellite service
func (s *Satellite) Stop() error {
	s.logger.Info().Msg("stopping satellite service")
	// TODO: Implement service shutdown logic
	return nil
}
