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

	ztrErrChan := make(chan<- error)
	stateErrChan := make(chan<- error)

	if !s.cm.IsZTRDone() {
		// schedule ztr
		go ScheduleFunc(ctx, log, s.cm.GetRegistrationInterval(), ztrProcess, ztrErrChan)
	}

	// schedule ztr
	go ScheduleFunc(ctx, log, s.cm.GetRegistrationInterval(), fetchAndReplicateStateProcess, stateErrChan)

	return nil
}

func ScheduleFunc(ctx context.Context, log *zerolog.Logger, interval string, process scheduler.Process, errChan chan<- error) {
	duration, _ := parseEveryExpr(interval)
	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Scheduler received cancellation signal. Exiting...")
			return
		case <-ticker.C:
			if !process.IsRunning() {
				log.Info().Str("Process Name", process.Name()).Msg("Scheduler triggering task execution")
				go func() {
					if err := process.Execute(ctx); err != nil {
						select {
						case errChan <- fmt.Errorf("%s failed: %w", process.Name(), err):
						default:
							log.Error().Err(err).Str("name", process.Name()).Msg("Dropped process error")
						}
					}
				}()
			} else {
				log.Debug().Str("Process Name", process.Name()).Msg("Process already executing")
			}
		}
	}
}

func parseEveryExpr(expr string) (time.Duration, error) {
	const prefix = "@every "
	if !strings.HasPrefix(expr, prefix) {
		return 0, fmt.Errorf("unsupported format: must start with %q", prefix)
	}
	return time.ParseDuration(strings.TrimPrefix(expr, prefix))
}
