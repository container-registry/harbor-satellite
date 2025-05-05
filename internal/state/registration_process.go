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
	"github.com/robfig/cron/v3"
)

const ZeroTouchRegistrationRoute = "satellites/ztr"
const ZeroTouchRegistrationEventName = "zero-touch-registration-event"

type ZtrProcess struct {
	// ID is the unique GetID of the process
	ID cron.EntryID
	// Name is the name of the process
	Name string
	// isRunning is true if the process is running
	isRunning bool
	// mu is the mutex to protect the process
	mu *sync.Mutex
	// eventBroker is the event broker to subscribe to the events
	eventBroker *scheduler.EventBroker
	// cronExpr is the cron expression for the process
	cronExpr string
	// Config manager to interact with the satellite config
	cm *config.ConfigManager
}

func NewZtrProcess(cm *config.ConfigManager) *ZtrProcess {
	return &ZtrProcess{
		Name:     config.ZTRConfigJobName,
		cronExpr: cm.GetRegistrationInterval(),
		mu:       &sync.Mutex{},
		cm:       cm,
	}
}

type ZeroTouchRegistrationEventPayload struct {
	StateConfig config.StateConfig
}

func (z *ZtrProcess) Execute(ctx context.Context) error {
	log := logger.FromContext(ctx)
	if !z.start() {
		log.Warn().Msgf("Process %s is already running", z.Name)
		return nil
	}
	defer z.stop()
	canExecute, reason := z.CanExecute(ctx)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", z.Name, reason)
		return nil
	}
	log.Info().Msgf("Executing process %s", z.Name)

	// Register the satellite
	stateConfig, err := RegisterSatellite(z.cm.GetGroundControlURL(), ZeroTouchRegistrationRoute, z.cm.GetToken(), ctx)
	if err != nil {
		log.Error().Msgf("Failed to register satellite: %v", err)
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

	zeroTouchRegistrationEvent := scheduler.Event{
		Name: ZeroTouchRegistrationEventName,
		Payload: ZeroTouchRegistrationEventPayload{
			StateConfig: stateConfig,
		},
		Source: z.Name,
	}
	if err := z.eventBroker.Publish(zeroTouchRegistrationEvent, ctx); err != nil {
		log.Error().Msgf("Failed to register satellite: could not emit ztr event")
		return fmt.Errorf("failed to register satellite: could not emit ztr event")
	}

	stopProcessPayload := scheduler.StopProcessEventPayload{
		ProcessName: z.GetName(),
		Id:          z.GetID(),
	}
	stopProcessEvent := scheduler.Event{
		Name:    scheduler.StopProcessEventName,
		Payload: stopProcessPayload,
		Source:  z.Name,
	}
	if err := z.eventBroker.Publish(stopProcessEvent, ctx); err != nil {
		log.Error().Msgf("Failed to register satellite: could not emit stop process event")
		return fmt.Errorf("failed to register satellite: could not emit stop process event")
	}

	return nil
}

func (z *ZtrProcess) GetID() cron.EntryID {
	return z.ID
}

func (z *ZtrProcess) SetID(id cron.EntryID) {
	z.ID = id
}

func (z *ZtrProcess) GetName() string {
	return z.Name
}

func (z *ZtrProcess) GetCronExpr() string {
	return z.cronExpr
}

func (z *ZtrProcess) IsRunning() bool {
	return z.isRunning
}

// CanExecute checks if the process can execute.
// It returns true if the process can execute, false otherwise.
func (z *ZtrProcess) CanExecute(ctx context.Context) (bool, string) {
	log := logger.FromContext(ctx)
	log.Info().Msgf("Checking if process %s can execute", z.Name)

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

	return true, fmt.Sprintf("Process %s can execute all conditions fulfilled", z.Name)
}

func (z *ZtrProcess) AddEventBroker(eventBroker *scheduler.EventBroker, context context.Context) {
	z.eventBroker = eventBroker
}

func (z *ZtrProcess) start() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.isRunning {
		return false
	}
	z.isRunning = true
	return true
}

func (z *ZtrProcess) stop() {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.isRunning = false
}

func RegisterSatellite(groundControlURL, path, token string, ctx context.Context) (config.StateConfig, error) {
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
