package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Scheduler manages the execution of processes with configurable intervals.
type Scheduler struct {
	name     string
	ticker   *time.Ticker
	process  Process
	log      *zerolog.Logger
	interval time.Duration
	// startupJitter bounds a random delay applied before the first run only.
	// Subsequent runs are driven by the ticker and are unaffected. It is zero
	// by default, preserving the existing run-immediately behaviour; callers
	// that manage a fleet opt in with WithStartupJitter so that satellites
	// booting together do not all call Ground Control at the same instant.
	startupJitter time.Duration
	mu            sync.Mutex
	wg            sync.WaitGroup
}

// NewSchedulerWithInterval creates a new scheduler with a parsed interval string.
func NewSchedulerWithInterval(intervalExpr string, process Process, log *zerolog.Logger) (*Scheduler, error) {
	duration, err := parseEveryExpr(intervalExpr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse interval: %w", err)
	}

	ticker := time.NewTicker(duration)
	scheduler := &Scheduler{
		name:     process.Name(),
		ticker:   ticker,
		process:  process,
		log:      log,
		interval: duration,
	}

	return scheduler, nil
}

// WithStartupJitter sets the upper bound of the random delay applied before the
// first run. The zero value, which is the default, runs immediately. Passing the
// scheduler interval spreads a fleet's first execution uniformly across one
// interval. Returns the scheduler so it can be chained onto the constructor.
func (s *Scheduler) WithStartupJitter(d time.Duration) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d < 0 {
		d = 0
	}
	s.startupJitter = d

	return s
}

// waitStartupJitter blocks for a random duration in [0, startupJitter) before
// the first run. It returns false if the context was cancelled while waiting.
func (s *Scheduler) waitStartupJitter(ctx context.Context) bool {
	s.mu.Lock()
	bound := s.startupJitter
	s.mu.Unlock()

	if bound <= 0 {
		return true
	}

	//nolint:gosec // load spreading, not a security boundary; math/rand is sufficient
	delay := time.Duration(rand.Int63n(int64(bound)))

	s.log.Debug().
		Str("Process", s.process.Name()).
		Dur("delay", delay).
		Msg("Delaying first execution to spread load across the fleet")

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Start launches Run in a goroutine with proper WaitGroup tracking.
// wg.Add must happen before the goroutine to avoid a race with Stop.
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

// run starts the scheduler and blocks until context is cancelled.
func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()
	defer s.ticker.Stop()

	s.log.Info().
		Str("Process", s.process.Name()).
		Dur("interval", s.interval).
		Msg("Starting scheduler")

	// Run once at startup, after a bounded random delay so that satellites
	// booting together do not all call Ground Control at the same instant.
	if !s.waitStartupJitter(ctx) {
		s.log.Info().
			Str("Process", s.process.Name()).
			Msg("Scheduler cancelled before first execution. Exiting...")

		return
	}

	// The ticker has been running since construction, so a tick may have been
	// buffered while the jitter delay elapsed. Reset it so the interval between
	// the first and second runs is a full interval rather than whatever remains.
	s.resetTickerAfterJitter()

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

// resetTickerAfterJitter restarts the ticker so that the delay between the
// first and second executions is a full interval. Without it, a tick buffered
// during the jitter wait fires immediately after the first run.
func (s *Scheduler) resetTickerAfterJitter() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startupJitter <= 0 {
		return
	}

	s.ticker.Reset(s.interval)
}

// ResetInterval changes the ticker interval dynamically.
func (s *Scheduler) ResetInterval(newInterval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ticker.Reset(newInterval)
	s.interval = newInterval
	s.log.Info().
		Str("Process", s.process.Name()).
		Dur("newInterval", newInterval).
		Msg("Scheduler interval reset")
}

// ResetIntervalFromExpr changes the ticker interval using an expression string.
func (s *Scheduler) ResetIntervalFromExpr(intervalExpr string) error {
	duration, err := parseEveryExpr(intervalExpr)
	if err != nil {
		return fmt.Errorf("failed to parse interval: %w", err)
	}

	s.ResetInterval(duration)

	return nil
}

// GetInterval returns the current interval.
func (s *Scheduler) GetInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.interval
}

// Name returns the name of the scheduler.
func (s *Scheduler) Name() string {
	return s.name
}

// Stop signals the scheduler to stop and waits for all goroutines to complete.
func (s *Scheduler) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) launchProcess(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.process.IsRunning() {
		s.log.Info().
			Str("Process", s.process.Name()).
			Msg("Scheduler triggering task execution")

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.process.Execute(ctx); err != nil {
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

func parseEveryExpr(expr string) (time.Duration, error) {
	const prefix = "@every "
	if expr == "" {
		return 0, errors.New("empty expression provided")
	}
	if !strings.HasPrefix(expr, prefix) {
		return 0, fmt.Errorf("unsupported format: must start with %q", prefix)
	}

	return time.ParseDuration(strings.TrimPrefix(expr, prefix))
}
