package events

import (
	"context"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/rs/zerolog"
)

type EventScheduler struct {
	mu  sync.Mutex
	log *zerolog.Logger

	eventMap map[string]*scheduler.Scheduler
	cancels  map[string]context.CancelFunc
	stopped  bool
}

type registration struct {
	sched  *scheduler.Scheduler
	cancel context.CancelFunc
}

func NewEventScheduler(log *zerolog.Logger) *EventScheduler {
	return &EventScheduler{
		log:      log,
		eventMap: make(map[string]*scheduler.Scheduler),
		cancels:  make(map[string]context.CancelFunc),
	}
}

func (s *EventScheduler) Register(ctx context.Context, sched *scheduler.Scheduler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sched == nil || s.stopped {
		return
	}
	s.log.Info().Msgf("registered event: %s", sched.Name())
	if cancel, ok := s.cancels[sched.Name()]; ok {
		cancel()
	}
	runCtx, cancel := context.WithCancel(ctx) //nolint:gosec // Stop calls the stored cancel
	s.eventMap[sched.Name()] = sched
	s.cancels[sched.Name()] = cancel

	sched.Start(runCtx)
}

func (s *EventScheduler) SendEvent(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	sched, ok := s.eventMap[event]
	if !ok {
		return
	}
	if !sched.TryTrigger() {
		s.log.Debug().Str("event", event).Msg("event already pending, dropped")
	}
}
