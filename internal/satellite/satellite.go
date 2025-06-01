package satellite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/state"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

type Satellite struct {
	cm *config.ConfigManager
}

func NewSatellite(cm *config.ConfigManager) *Satellite {
	return &Satellite{
		cm: cm,
	}
}

func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msg("Starting Satellite")

	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(s.cm)
	ztrProcess := state.NewZtrProcess(s.cm)

	if !s.cm.IsZTRDone() {
		// schedule ztr
		go ScheduleFunc(ctx, log, s.cm.GetRegistrationInterval(), ztrProcess)

		select {
		case <-ztrProcess.Done:
			log.Info().Msg("ZTR process completed, scheduling the other processes...")
		case <-ctx.Done():
			log.Info().Msg("Satellite context cancelled, shutting down...")
			return ctx.Err()
		}
	}

	// schedule state replication
	go ScheduleFunc(ctx, log, s.cm.GetStateReplicationInterval(), fetchAndReplicateStateProcess)

	// Wait until context is cancelled
	<-ctx.Done()
	log.Info().Msg("Satellite context cancelled, shutting down...")

	return ctx.Err()
}

// TODO: lets pass the ticker directly to the scheduler. We can reset the ticker which streamlines everything.
func ScheduleFunc(ctx context.Context, log *zerolog.Logger, interval string, process scheduler.Process) {
	duration, _ := parseEveryExpr(interval)
	ticker := time.NewTicker(duration)
	schedulerLogger := log.With().Str("component", "process scheduler").Str("Process", process.Name()).Logger()
	defer ticker.Stop()

	log.Info().Msgf("Task will be performed at every %s", interval)

	// Run once immediately
	launchProcess(ctx, schedulerLogger, process)

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Scheduler received cancellation signal. Exiting...")
			return
		case <-ticker.C:
			if process.IsComplete() {
				log.Info().Msg("Process marked as complete. Stopping scheduling.")
				return
			}
			launchProcess(ctx, schedulerLogger, process)
		}
	}
}

func launchProcess(ctx context.Context, log zerolog.Logger, process scheduler.Process) {
	if !process.IsRunning() {
		log.Info().Msg("Scheduler triggering task execution")
		go func() {
			if err := process.Execute(ctx); err != nil {
				log.Warn().Str("Process", process.Name()).Err(err).Msg("Error occurred while executing process.")
			}
		}()
	} else {
		log.Debug().Msg("Skipping execution of process since a previous execution cycle is still running")
	}
}

func parseEveryExpr(expr string) (time.Duration, error) {
	const prefix = "@every "
	if expr == "" {
		return 0, fmt.Errorf("empty expression provided")
	}

	if !strings.HasPrefix(expr, prefix) {
		return 0, fmt.Errorf("unsupported format: must start with %q", prefix)
	}
	return time.ParseDuration(strings.TrimPrefix(expr, prefix))
}
