package satellite

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/state"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

type Satellite struct {
	cm         *config.ConfigManager
	schedulers []*scheduler.Scheduler
}

func NewSatellite(cm *config.ConfigManager) *Satellite {
	return &Satellite{
		cm:         cm,
		schedulers: make([]*scheduler.Scheduler, 0),
	}
}

func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msg("Starting Satellite")

	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(s.cm)
	ztrProcess := state.NewZtrProcess(s.cm)

	// Create schedulers instead of using ScheduleFunc
	if !s.cm.IsZTRDone() {
		ztrScheduler, err := scheduler.NewSchedulerWithInterval(
			s.cm.GetRegistrationInterval(),
			ztrProcess,
			log,
		)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create ZTR scheduler")
			return err
		}
		s.schedulers = append(s.schedulers, ztrScheduler)
		go ztrScheduler.Run(ctx)
	}

	// Create state replication scheduler
	stateScheduler, err := scheduler.NewSchedulerWithInterval(
		s.cm.GetStateReplicationInterval(),
		fetchAndReplicateStateProcess,
		log,
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create state replication scheduler")
		return err
	}
	s.schedulers = append(s.schedulers, stateScheduler)
	go stateScheduler.Run(ctx)

	return ctx.Err()
}

func (s *Satellite) GetSchedulers() []*scheduler.Scheduler {
	return s.schedulers
}

// Stop gracefully stops all schedulers
func (s *Satellite) Stop() {
	for _, scheduler := range s.schedulers {
		scheduler.Stop()
	}
}
