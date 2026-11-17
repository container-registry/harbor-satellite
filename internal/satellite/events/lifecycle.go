package events

import (
	"context"
	"errors"
	"fmt"
)

// Stop stops every registered scheduler and waits for running events until
// ctx ends; later SendEvent calls are dropped (plan item 6.7). Register
// therefore derives a cancelable context per scheduler and keeps its cancel
// func, because Scheduler.Stop alone does not end the scheduler's run loop.
// SendEvent reads the registrations under the same lock as Register and
// Stop, and triggers with TryTrigger so it never blocks the heartbeat.
func (s *EventScheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	scheds := make(map[string]registration, len(s.eventMap))
	for name, sched := range s.eventMap {
		scheds[name] = registration{sched: sched, cancel: s.cancels[name]}
	}
	s.mu.Unlock()

	var errs []error
	for name, r := range scheds {
		if r.cancel != nil {
			r.cancel()
		}
		if err := r.sched.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop event %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
