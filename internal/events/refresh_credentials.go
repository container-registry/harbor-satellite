package events

import (
	"context"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/rs/zerolog"
)

// TODO: Complete Process

type RefreshCredentialProcess struct {
	name       string
	isRunning  bool
	isComplete bool

	mu sync.RWMutex
}

func NewRefreshCredentialsEvent(log *zerolog.Logger) (*scheduler.Scheduler, error) {
	sched, err := scheduler.NewScheduler(&RefreshCredentialProcess{
		name:       "refresh_credentials",
		isRunning:  false,
		isComplete: false,
	}, log)
	if err != nil {
		return nil, err
	}

	return sched, nil
}

func (s *RefreshCredentialProcess) Execute(ctx context.Context) error {
	s.start()
	defer s.stop()

	return nil
}

func (s *RefreshCredentialProcess) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

func (s *RefreshCredentialProcess) IsComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isComplete
}

func (s *RefreshCredentialProcess) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

func (s *RefreshCredentialProcess) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = true
}

func (s *RefreshCredentialProcess) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = false
}

// func (s *RefreshCredentialProcess) complete() {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()
// 	s.isComplete = true
// }
