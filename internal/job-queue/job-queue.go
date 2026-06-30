package jobqueue

import (
	"context"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/scheduler"
)

// TODO: Add job cancellation handling
// TODO: Add goroutine limits

type JobQueue struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex

	actionChan chan string
	actionMap  map[string]scheduler.Process
}

func NewJobQueue(buffer int) *JobQueue {
	ctx, cancel := context.WithCancel(context.Background())

	return &JobQueue{
		ctx:        ctx,
		cancel:     cancel,
		actionChan: make(chan string, buffer),
		actionMap:  make(map[string]scheduler.Process),
	}
}

func (s *JobQueue) Register(name string, p scheduler.Process) {
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
					v.Execute(context.Background())
				}
			}
		}
	}()
}
