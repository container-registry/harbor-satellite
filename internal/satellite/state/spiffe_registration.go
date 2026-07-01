package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/spiffe"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

const SPIFFEZeroTouchRegistrationRoute = "satellites/spiffe-ztr"

type SpiffeZtrProcess struct {
	name         string
	isRunning    bool
	mu           *sync.Mutex
	Done         chan struct{}
	cm           *config.ConfigManager
	spiffeClient *spiffe.Client
}

func NewSpiffeZtrProcess(cm *config.ConfigManager) (*SpiffeZtrProcess, error) {
	spiffeCfg := cm.GetSPIFFEConfig()
	if !spiffeCfg.Enabled {
		return nil, fmt.Errorf("SPIFFE is not enabled in config")
	}

	client, err := spiffe.NewClient(spiffe.Config{
		Enabled:          spiffeCfg.Enabled,
		EndpointSocket:   spiffeCfg.EndpointSocket,
		ExpectedServerID: spiffeCfg.ExpectedServerID,
	})
	if err != nil {
		return nil, fmt.Errorf("create SPIFFE client: %w", err)
	}

	return &SpiffeZtrProcess{
		name:         config.SPIFFEZTRConfigJobName,
		mu:           &sync.Mutex{},
		cm:           cm,
		Done:         make(chan struct{}, 1),
		spiffeClient: client,
	}, nil
}

func (s *SpiffeZtrProcess) Execute(ctx context.Context) error {
	s.start()
	defer s.stop()

	log := logger.FromContext(ctx).With().Str("process", s.name).Logger()

	canExecute, reason := s.CanExecute(&log)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", s.name, reason)
		return nil
	}
	log.Info().Msg("Executing SPIFFE-based ZTR process")

	if err := s.spiffeClient.Connect(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to connect to SPIRE agent")
		return fmt.Errorf("connect to SPIRE agent: %w", err)
	}

	spiffeID, err := s.spiffeClient.GetSPIFFEID()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get SPIFFE ID")
		return fmt.Errorf("get SPIFFE ID: %w", err)
	}
	log.Info().Str("spiffe_id", spiffeID.String()).Msg("Obtained SPIFFE identity")

	stateConfig, err := s.registerWithSPIFFE(ctx, &log)
	if err != nil {
		log.Error().Err(err).Msg("Failed to register satellite via SPIFFE")
		return err
	}

	if stateConfig.RegistryCredentials.Username == "" ||
		stateConfig.RegistryCredentials.Password == "" ||
		stateConfig.RegistryCredentials.URL == "" ||
		stateConfig.StateURL == "" {
		log.Error().Msg("Invalid state auth config received")
		return fmt.Errorf("invalid state auth config received")
	}

	s.cm.With(config.SetStateConfig(stateConfig))
	if err := s.cm.WriteConfig(); err != nil {
		log.Error().Err(err).Msg("Failed to write config")
		return fmt.Errorf("write config: %w", err)
	}

	log.Info().Msg("SPIFFE-based ZTR completed successfully")
	close(s.Done)
	return nil
}

func (s *SpiffeZtrProcess) registerWithSPIFFE(ctx context.Context, log *zerolog.Logger) (config.StateConfig, error) {
	gcURL := s.cm.ResolveGroundControlURL()
	ztrURL := fmt.Sprintf("%s/%s", gcURL, SPIFFEZeroTouchRegistrationRoute)

	httpClient, err := s.spiffeClient.CreateHTTPClient()
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("create SPIFFE HTTP client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ztrURL, nil)
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("create request: %w", err)
	}

	log.Debug().Str("url", ztrURL).Msg("Sending SPIFFE-authenticated ZTR request")
	resp, err := httpClient.Do(req)
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("send request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return config.StateConfig{}, fmt.Errorf("registration failed: %s", resp.Status)
	}

	var stateConfig config.StateConfig
	if err := json.NewDecoder(resp.Body).Decode(&stateConfig); err != nil {
		return config.StateConfig{}, fmt.Errorf("decode response: %w", err)
	}

	return stateConfig, nil
}

func (s *SpiffeZtrProcess) CanExecute(log *zerolog.Logger) (bool, string) {
	log.Info().Msgf("Checking if process %s can execute", s.name)

	if s.cm.ResolveGroundControlURL() == "" {
		return false, "missing ground control URL"
	}

	if !s.cm.IsSPIFFEEnabled() {
		return false, "SPIFFE is not enabled"
	}

	return true, fmt.Sprintf("Process %s can execute all conditions fulfilled", s.name)
}

func (s *SpiffeZtrProcess) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

func (s *SpiffeZtrProcess) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunning
}

func (s *SpiffeZtrProcess) IsComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cm.IsZTRDone()
}

func (s *SpiffeZtrProcess) start() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = true
	return true
}

func (s *SpiffeZtrProcess) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = false
}

func (s *SpiffeZtrProcess) Close() error {
	if s.spiffeClient != nil {
		return s.spiffeClient.Close()
	}
	return nil
}
