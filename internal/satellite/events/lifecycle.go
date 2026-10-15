package events

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// Stop stops every registered scheduler and waits for running events until
// ctx ends; later SendEvent calls are dropped (plan item 6.7). Register
// therefore derives a cancelable context per scheduler and keeps its cancel
// func, because Scheduler.Stop alone does not end the scheduler's run loop.
// SendEvent reads the registrations under the same lock as Register and
// Stop, and triggers with TryTrigger so it never blocks the heartbeat.
func (s *EventScheduler) Stop(ctx context.Context) error {
	return notimpl.Err("6.7 stop event schedulers")
}
