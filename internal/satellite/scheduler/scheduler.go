package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

// RunResult records the outcome of a single execution of a scheduled process.
type RunResult struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Err        error
}

// Duration returns how long the run took.
func (r RunResult) Duration() time.Duration {
	return r.FinishedAt.Sub(r.StartedAt)
}

// Success reports whether the run completed without error.
func (r RunResult) Success() bool {
	return r.Err == nil
}

// Scheduler manages the execution of processes with configurable intervals.
type Scheduler struct {
	name     string
	ticker   *time.Ticker
	process  Process
	log      *zerolog.Logger
	interval time.Duration
	mu       sync.Mutex
	wg       sync.WaitGroup
	history   []RunResult
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
			if s.process.ShouldStop() {
				s.log.Info().
					Str("Process", s.process.Name()).
					Msg("Process marked as complete. Stopping scheduling.")

				return
			}
			s.launchProcess(ctx)
		}
	}
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

			result := RunResult{StartedAt: time.Now()}
			result.Err = s.process.Execute(ctx)
			result.FinishedAt = time.Now()
			s.recordRun(result)

			logEvent := s.log.Info()
			if result.Err != nil {
            	logEvent = s.log.Warn()
			}
			logEvent.
            	Str("Process", s.process.Name()).
             	Dur("duration", result.Duration()).
              	Bool("success", result.Success()).
               	AnErr("error", result.Err).
                Msg("Process run completed")
		}()
	} else {
		s.log.Debug().
			Str("Process", s.process.Name()).
			Msg("Process already executing")
	}
}

// recordRun appends a run result, discarding the oldest entry once
// maxRunHistory is exceeded.
func (s *Scheduler) recordRun(result RunResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = append(s.history, result)
	if len(s.history) > config.MaxRunHistory {
		s.history = s.history[len(s.history)-config.MaxRunHistory:]
	}
}

// History returns a copy of the retained run results, oldest first.
func (s *Scheduler) History() []RunResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]RunResult, len(s.history))
	copy(out, s.history)
	return out
}

// LastRun returns the most recent run result and true, or a zero value
// and false if the process has not executed yet.
func (s *Scheduler) LastRun() (RunResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.history) == 0 {
		return RunResult{}, false
	}
	return s.history[len(s.history)-1], true
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