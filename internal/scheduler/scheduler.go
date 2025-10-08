package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type UpstreamInfo struct {
	LatestStateDigest  string
	LatestConfigDigest string
	CurrentActivity    string
	StateURL           string
}

// Scheduler manages the execution of processes with configurable intervals
type Scheduler struct {
	name            string
	ticker          *time.Ticker
	process         Process
	log             *zerolog.Logger
	interval        time.Duration
	mu              sync.Mutex
	upstreamPayload *UpstreamInfo
}

const (
	ActivityStateSynced      = "state synced successfully"
	ActivityEncounteredError = "encountered error"
	ActivityReconcilingState = "reconciling state"
)

// NewSchedulerWithInterval creates a new scheduler with a parsed interval string
func NewSchedulerWithInterval(intervalExpr string, process Process, log *zerolog.Logger, upstreamPayload *UpstreamInfo) (*Scheduler, error) {
	duration, err := ParseEveryExpr(intervalExpr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse interval: %w", err)
	}

	ticker := time.NewTicker(duration)
	scheduler := &Scheduler{
		name:            process.Name(),
		ticker:          ticker,
		process:         process,
		log:             log,
		interval:        duration,
		upstreamPayload: upstreamPayload,
	}

	return scheduler, nil
}

// Run starts the scheduler and blocks until context is cancelled
func (s *Scheduler) Run(ctx context.Context) {
	defer s.ticker.Stop()

	s.log.Info().
		Str("Process", s.process.Name()).
		Dur("interval", s.interval).
		Msg("Starting scheduler")

	// Run once immediately
	s.launchProcess(ctx)

	for {
		select {
		case <-ctx.Done():
			s.log.Info().
				Str("Process", s.process.Name()).
				Msg("Scheduler received cancellation signal. Exiting...")
			return

		case <-s.ticker.C:
			if s.process.IsComplete() {
				s.log.Info().
					Str("Process", s.process.Name()).
					Msg("Process marked as complete. Stopping scheduling.")
				return
			}
			s.launchProcess(ctx)
		}
	}
}

// ResetInterval changes the ticker interval dynamically
func (s *Scheduler) ResetInterval(newInterval time.Duration) {
	s.ticker.Reset(newInterval)
	s.interval = newInterval
	s.log.Info().
		Str("Process", s.process.Name()).
		Dur("newInterval", newInterval).
		Msg("Scheduler interval reset")
}

// ResetIntervalFromExpr changes the ticker interval using an expression string
func (s *Scheduler) ResetIntervalFromExpr(intervalExpr string) error {
	duration, err := ParseEveryExpr(intervalExpr)
	if err != nil {
		return fmt.Errorf("failed to parse interval: %w", err)
	}

	s.ResetInterval(duration)
	return nil
}

// GetInterval returns the current interval
func (s *Scheduler) GetInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interval
}

// Name returns the name of the scheduler
func (s *Scheduler) Name() string {
	return s.name
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.ticker.Stop()
}

func (s *Scheduler) launchProcess(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.process.IsRunning() {
		s.log.Info().
			Str("Process", s.process.Name()).
			Msg("Scheduler triggering task execution")

		go func() {
			if err := s.process.Execute(ctx, s.upstreamPayload); err != nil {
				s.log.Warn().
					Str("Process", s.process.Name()).
					Err(err).
					Msg("Error occurred while executing process.")
			}
		}()
	} else {
		s.log.Debug().
			Str("Process", s.process.Name()).
			Msg("Process already executing")
	}
}

func ParseEveryExpr(expr string) (time.Duration, error) {
	const prefix = "@every "
	if expr == "" {
		return 0, fmt.Errorf("empty expression provided")
	}
	if !strings.HasPrefix(expr, prefix) {
		return 0, fmt.Errorf("unsupported format: must start with %q", prefix)
	}
	return time.ParseDuration(strings.TrimPrefix(expr, prefix))
}
