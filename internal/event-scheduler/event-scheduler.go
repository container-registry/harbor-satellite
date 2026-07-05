package eventscheduler

import (
	"context"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/rs/zerolog"
)

// TODO: Add job cancellation handling
// TODO: Add goroutine limits
// TODO: Event Queue

type EventScheduler struct {
	mu  sync.Mutex
	log *zerolog.Logger

	eventMap map[string]*scheduler.Scheduler
}

func NewEventScheduler() *EventScheduler {
	log := logger.FromContext(context.Background()).With().Str("process", "job_queue").Logger()

	return &EventScheduler{
		log:      &log,
		eventMap: make(map[string]*scheduler.Scheduler),
	}
}

func (s *EventScheduler) Register(sched *scheduler.Scheduler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info().Msgf("registered event: %s", sched.Name())
	s.eventMap[sched.Name()] = sched

	sched.Start(context.Background())
}

func (s *EventScheduler) SendEvent(event string) {
	s.log.Info().Msgf("executing event %s: ", event)
	sched, ok := s.eventMap[event]
	if !ok {
		s.log.Warn().Msgf("event not found: %s", event)
		return
	}

	s.log.Info().Msgf("executing event: %s", event)
	sched.Trigger()
}
