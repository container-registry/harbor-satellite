package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

const ZeroTouchRegistrationRoute = "satellites/ztr"
const ZeroTouchRegistrationEventName = "zero-touch-registration-event"

type ZtrProcess struct {
	// Name is the name of the process
	name string
	// isRunning is true if the process is running
	isRunning bool
	// mu is the mutex to protect the process
	mu *sync.Mutex
	// done chan is used to communicate about the success of the ZtrProcess
	Done chan struct{}
	// Config manager to interact with the satellite config
	cm *config.ConfigManager
}

func NewZtrProcess(cm *config.ConfigManager) *ZtrProcess {
	return &ZtrProcess{
		name: config.ZTRConfigJobName,
		mu:   &sync.Mutex{},
		cm:   cm,
		Done: make(chan struct{}, 1),
	}
}

func (z *ZtrProcess) Execute(ctx context.Context, upstreamPayload *scheduler.UpstreamInfo) error {
	z.start()
	defer z.stop()

	log := logger.FromContext(ctx).With().Str("process", z.name).Logger()

	canExecute, reason := z.CanExecute(&log)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", z.name, reason)
		return nil
	}
	log.Info().Msgf("Executing process")

	// Register the satellite
	stateConfig, err := registerSatellite(z.cm.ResolveGroundControlURL(), ZeroTouchRegistrationRoute, z.cm.GetToken(), ctx)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to register satellite")
		return err
	}

	if stateConfig.RegistryCredentials.Username == "" || stateConfig.RegistryCredentials.Password == "" || stateConfig.RegistryCredentials.URL == "" || stateConfig.StateURL == "" {
		log.Error().Msgf("Failed to register satellite: invalid state auth config received")
		return fmt.Errorf("failed to register satellite: invalid state auth config received")
	}

	// Update the state config in app config
	z.cm.With(config.SetStateConfig(stateConfig))
	if err := z.cm.WriteConfig(); err != nil {
		log.Error().Msgf("Failed to register satellite: could not update state auth config")
		return fmt.Errorf("failed to register satellite: could not update state auth config")
	}

	// Close the z.Done channel on successful ZTR alone.
	close(z.Done)

	return nil
}

// CanExecute checks if the process can execute.
// It returns true if the process can execute, false otherwise.
func (z *ZtrProcess) CanExecute(log *zerolog.Logger) (bool, string) {
	log.Info().Msgf("Checking if process %s can execute", z.name)

	checks := []struct {
		condition bool
		message   string
	}{
		{z.cm.GetToken() == "", "token"},
		{z.cm.ResolveGroundControlURL() == "", "ground control URL"},
	}
	var missing []string
	for _, check := range checks {
		if check.condition {
			missing = append(missing, check.message)
		}
	}
	if len(missing) > 0 {
		return false, fmt.Sprintf("missing %s, please include the required environment variables present in .env", strings.Join(missing, ", "))
	}

	return true, fmt.Sprintf("Process %s can execute all conditions fulfilled", z.name)
}

func (z *ZtrProcess) Name() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.name
}

func (z *ZtrProcess) IsRunning() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.isRunning
}

func (z *ZtrProcess) IsComplete() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.cm.IsZTRDone()
}

func (z *ZtrProcess) start() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.isRunning = true
	return true
}

func (z *ZtrProcess) stop() {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.isRunning = false
}

func registerSatellite(groundControlURL, path, token string, ctx context.Context) (config.StateConfig, error) {
	ztrURL := fmt.Sprintf("%s/%s/%s", groundControlURL, path, token)
	client := &http.Client{}

	// Create a new request for the Zero Touch Registration of satellite
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ztrURL, nil)
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("failed to create request: %w", err)
	}
	response, err := client.Do(req)
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("failed to send request: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return config.StateConfig{}, fmt.Errorf("failed to register satellite: %s", response.Status)
	}

	var authResponse config.StateConfig
	if err := json.NewDecoder(response.Body).Decode(&authResponse); err != nil {
		return config.StateConfig{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return authResponse, nil
}
