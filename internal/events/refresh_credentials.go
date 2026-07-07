package events

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

// TODO: Complete Process

type RefreshCredentialProcess struct {
	name       string
	isRunning  bool
	isComplete bool
	cm         *config.ConfigManager

	mu sync.RWMutex
}

type RefreshEndpointResponse struct {
	Secret string `json:"secret"`
}

func NewRefreshCredentialsEvent(cm *config.ConfigManager, log *zerolog.Logger) (*scheduler.Scheduler, error) {
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

	if s.cm == nil {
		return fmt.Errorf("config manager not found")
	}

	gcURL := s.cm.ResolveGroundControlURL()
	reqURL := fmt.Sprintf("%s/api/refresh", gcURL)

	resp, err := http.Get(reqURL)
	if err != nil {
		return fmt.Errorf("request failed with error: %v", err)
	}
	defer resp.Body.Close()

	var respBody RefreshEndpointResponse
	err = json.NewDecoder(resp.Body).Decode(&respBody)
	if err != nil {
		return fmt.Errorf("failed to decode body: %v", err)
	}

	// TODO: Add sanitation check for the token

	setter := config.SetStateAuth(s.cm.GetSourceRegistryUsername(), respBody.Secret, config.URL(s.cm.GetSourceRegistryURL()))
	setter(s.cm.GetConfig())

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

// TODO: To be addressed in a separate PR
// func (s *RefreshCredentialProcess) complete() {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()
// 	s.isComplete = true
// }
