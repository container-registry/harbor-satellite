package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/internal/spiffe"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

type RefreshCredentialProcess struct {
	name         string
	isRunning    bool
	isComplete   bool
	cm           *config.ConfigManager
	log          *zerolog.Logger
	spiffeClient *spiffe.Client

	mu sync.RWMutex
}

type RefreshEndpointResponse struct {
	Secret string `json:"secret"`
}

func NewRefreshCredentialsEvent(cm *config.ConfigManager, log *zerolog.Logger) (*scheduler.Scheduler, error) {
	proc := &RefreshCredentialProcess{
		name:       "refresh_credentials",
		isRunning:  false,
		isComplete: false,
		cm:         cm,
		log:        log,
	}

	if cm.IsSPIFFEEnabled() {
		spiffeCfg := cm.GetSPIFFEConfig()
		client, err := spiffe.NewClient(spiffe.Config{
			Enabled:          spiffeCfg.Enabled,
			EndpointSocket:   spiffeCfg.EndpointSocket,
			ExpectedServerID: spiffeCfg.ExpectedServerID,
		})
		if err == nil {
			proc.spiffeClient = client
		}
	}

	sched, err := scheduler.NewScheduler(proc, log)
	if err != nil {
		return nil, err
	}

	return sched, nil
}

func (s *RefreshCredentialProcess) Execute(ctx context.Context) error {
	s.start()
	defer s.stop()

	// log := logger.FromContext(context.Background()).With().Str("process", s.name).Logger()

	if s.cm == nil {
		return fmt.Errorf("config manager not found")
	}

	resp, err := s.sendRequest(ctx)
	if err != nil {
		return fmt.Errorf("error sending refresh request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.FromContext(ctx).Warn().Err(err).Msg("error closing response body")
		}
	}()
	// log.Printf("Status: %s", resp.Status)
	// log.Printf("Body: %s", r)
	//
	// var respBody RefreshEndpointResponse
	// err = json.NewDecoder(resp.Body).Decode(&respBody)
	// if err != nil {
	// 	return fmt.Errorf("failed to decode body: %v", err)
	// }

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	s.log.Info().
		Str("status", resp.Status).
		Str("body", string(body)).
		Msg("Refresh endpoint response")

	var respBody RefreshEndpointResponse
	if err := json.Unmarshal(body, &respBody); err != nil {
		return fmt.Errorf("failed to decode body: %w", err)
	}

	setter := config.SetStateAuth(s.cm.GetSourceRegistryUsername(), respBody.Secret, config.URL(s.cm.GetSourceRegistryURL()))
	setter(s.cm.GetConfig())

	s.log.Info().Msgf("Secret After Update: %s", s.cm.GetStateConfig().RegistryCredentials.Password)

	return nil
}

func (s *RefreshCredentialProcess) sendRequest(ctx context.Context) (*http.Response, error) {
	gcURL := s.cm.ResolveGroundControlURL()
	reqURL := fmt.Sprintf("%s/api/refresh", gcURL)

	var client *http.Client
	if s.spiffeClient != nil {
		if err := s.spiffeClient.Connect(ctx); err != nil {
			return nil, fmt.Errorf("connect to SPIRE agent: %w", err)
		}
		httpClient, err := s.spiffeClient.CreateHTTPClient()
		client = httpClient
		if err != nil {
			return nil, fmt.Errorf("create SPIFFE HTTP client: %w", err)
		}
	} else {
		httpClient, err := createHTTPClient(s.cm.GetTLSConfig(), s.cm.UseUnsecure())
		if err != nil {
			return nil, fmt.Errorf("create HTTP client: %w", err)
		}

		client = httpClient
	}

	httpReq, err := s.createRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	return resp, nil
}

func (s *RefreshCredentialProcess) createRequest(ctx context.Context, reqURL string) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if s.spiffeClient == nil {
		if !s.cm.UseUnsecure() && !strings.HasPrefix(reqURL, "https://") {
			return nil, fmt.Errorf("insecure connection: sync URL %q must use HTTPS when use_unsecure is false", reqURL)
		}

		username := s.cm.GetSourceRegistryUsername()
		password := s.cm.GetSourceRegistryPassword()
		s.log.Info().Msgf("Using username: %s, password: %s", username, password)
		httpReq.SetBasicAuth(username, password)
	}

	return httpReq, nil
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
