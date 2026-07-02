package actions

import (
	"context"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/process"
)

// TODO: Complete Process

type RefreshCredentialProcess struct {
	name       string
	isRunning  bool
	isComplete bool
	errs       []error

	mu sync.RWMutex
}

func NewRefreshCredentialsAction() (string, process.Process) {
	return "refresh_credentials", &RefreshCredentialProcess{name: "Refresh Credentials"}
}

func (s *RefreshCredentialProcess) Execute(ctx context.Context) error {
	s.start()
	defer s.stop()
	defer s.complete()

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

func (s *RefreshCredentialProcess) complete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isComplete = true
}
