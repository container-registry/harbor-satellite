package satellite

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/logger"
	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/internal/satellite/state"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

type Satellite struct {
	cm            *config.ConfigManager
	criResults    []runtime.CRIConfigResult
	schedulers    []*scheduler.Scheduler
	stateFilePath string
	stateProcess  *state.FetchAndReplicateStateProcess
}

func NewSatellite(cm *config.ConfigManager, criResults []runtime.CRIConfigResult, stateFilePath string) *Satellite {
	return &Satellite{
		cm:            cm,
		criResults:    criResults,
		schedulers:    make([]*scheduler.Scheduler, 0),
		stateFilePath: stateFilePath,
	}
}

func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msg("Starting Satellite")

	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(s.cm, s.stateFilePath, log)
	s.stateProcess = fetchAndReplicateStateProcess

	// Create ZTR scheduler if not already done
	if !s.cm.IsZTRDone() {
		var ztrScheduler *scheduler.Scheduler
		var err error

		if s.cm.IsSPIFFEEnabled() {
			log.Info().Msg("SPIFFE authentication enabled, using SPIFFE-based ZTR")
			spiffeZtrProcess, processErr := state.NewSpiffeZtrProcess(s.cm)
			if processErr != nil {
				log.Error().Err(processErr).Msg("Failed to create SPIFFE ZTR process")
				return processErr
			}
			ztrScheduler, err = scheduler.NewSchedulerWithInterval(
				s.cm.GetRegistrationInterval(),
				spiffeZtrProcess,
				log,
			)
		} else {
			log.Info().Msg("Using token-based ZTR")
			ztrProcess := state.NewZtrProcess(s.cm)
			ztrScheduler, err = scheduler.NewSchedulerWithInterval(
				s.cm.GetRegistrationInterval(),
				ztrProcess,
				log,
			)
		}

		if err != nil {
			log.Error().Err(err).Msg("Failed to create ZTR scheduler")
			return err
		}
		s.schedulers = append(s.schedulers, ztrScheduler)
		ztrScheduler.Start(ctx)
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
	stateScheduler.Start(ctx)

	// Create status report scheduler with pending CRI results
	statusReportProcess := state.NewStatusReportingProcess(s.cm)
	if len(s.criResults) > 0 {
		statusReportProcess.SetPendingCRIResults(s.criResults)
	}
	statusScheduler, err := scheduler.NewSchedulerWithInterval(
		s.cm.GetHeartbeatInterval(),
		statusReportProcess,
		log,
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create status report scheduler")
		return err
	}
	s.schedulers = append(s.schedulers, statusScheduler)
	statusScheduler.Start(ctx)

	return ctx.Err()
}

func (s *Satellite) GetSchedulers() []*scheduler.Scheduler {
	return s.schedulers
}

// PersistState writes the current in-memory state to disk.
// Called during graceful shutdown to ensure no state is lost.
func (s *Satellite) PersistState() error {
	if s.stateProcess == nil {
		return nil
	}
	return s.stateProcess.PersistState()
}

// Stop gracefully stops all schedulers and logs the shutdown process.
func (s *Satellite) Stop(ctx context.Context) {
	log := logger.FromContext(ctx)
	log.Info().Int("scheduler_count", len(s.schedulers)).
		Msg("Initiating scheduler shutdown")

	failedCount := 0
	for i, sched := range s.schedulers {
		log.Debug().
			Int("index", i).
			Str("scheduler", sched.Name()).
			Msg("Stopping scheduler")
		if err := sched.Stop(ctx); err != nil {
			failedCount++
			log.Warn().Err(err).Str("scheduler", sched.Name()).Msg("Scheduler stop failed")
		}
	}

	if failedCount > 0 {
		log.Warn().Int("failed_count", failedCount).Msg("Some schedulers failed to stop")
	} else {
		log.Info().Msg("All schedulers stopped")
	}
}
