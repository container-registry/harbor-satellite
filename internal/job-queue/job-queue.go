package jobqueue

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

type JobQueue struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	log    *zerolog.Logger

	actionChan chan string
	actionMap  map[string]process.Process
}

func NewJobQueue(buffer int) *JobQueue {
	ctx, cancel := context.WithCancel(context.Background())
	log := logger.FromContext(ctx).With().Str("process", "job_queue").Logger()

	return &JobQueue{
		ctx:        ctx,
		cancel:     cancel,
		log:        &log,
		actionChan: make(chan string, buffer),
		actionMap:  make(map[string]process.Process),
	}
}

func (s *JobQueue) Register(name string, p process.Process) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.actionMap[name] = p
}

func (s *JobQueue) Start() {
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

func (s *JobQueue) SendAction(action string) {
	s.actionChan <- action
}
