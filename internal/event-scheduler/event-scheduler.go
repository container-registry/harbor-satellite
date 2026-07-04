package eventscheduler

import (
	"context"
	"fmt"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/process"
	"github.com/rs/zerolog"
)

// TODO: Add job cancellation handling
// TODO: Add goroutine limits
// TODO: Event Queue

type EventScheduler struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	log    *zerolog.Logger

	actionChan chan string
	actionMap  map[string]process.Process
}

func NewEventScheduler(buffer int) *EventScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	log := logger.FromContext(ctx).With().Str("process", "job_queue").Logger()

	return &EventScheduler{
		ctx:        ctx,
		cancel:     cancel,
		log:        &log,
		actionChan: make(chan string, buffer),
		actionMap:  make(map[string]process.Process),
	}
}

func (s *EventScheduler) Register(name string, p process.Process) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.actionMap[name] = p
}

func (s *EventScheduler) Start() {
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return

			case action := <-s.actionChan:
				if v, ok := s.actionMap[action]; ok {
					// TODO: Wrap for error retreival via chan? something for retreival
					// go v.Execute(context.Background())

					s.log.Info().Msg(fmt.Sprintf("Action Received: %s", v))
				}

				//TODO: Add Logging for failure
			}
		}
	}()
}

func (s *EventScheduler) SendAction(action string) {
	s.actionChan <- action
}
