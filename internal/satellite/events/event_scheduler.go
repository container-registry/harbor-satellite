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
}

func NewEventScheduler(log *zerolog.Logger) *EventScheduler {
	return &EventScheduler{
		log:      log,
		eventMap: make(map[string]*scheduler.Scheduler),
	}
}

func (s *EventScheduler) Register(ctx context.Context, sched *scheduler.Scheduler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info().Msgf("registered event: %s", sched.Name())
	s.eventMap[sched.Name()] = sched

	sched.Start(ctx)
}

func (s *EventScheduler) SendEvent(event string) {
	sched, ok := s.eventMap[event]
	if !ok {
		return
	}

	sched.Trigger()
}
