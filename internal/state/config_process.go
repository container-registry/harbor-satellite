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

const FetchConfigFromGroundControlEventName string = "fetch-config-from-ground-control-event"

const GroundControlSyncPath string = "configs"

type FetchConfigFromGroundControlProcess struct {
	id               cron.EntryID
	name             string
	cronExpr         string
	isRunning        bool
	token            string
	groundControlURL string
	mu               *sync.Mutex
	eventBroker      *scheduler.EventBroker
}

func NewFetchConfigFromGroundControlProcess(cronExpr string, token string, groundControlURL string) *FetchConfigFromGroundControlProcess {
	return &FetchConfigFromGroundControlProcess{
		name:             config.UpdateConfigJobName,
		cronExpr:         cronExpr,
		isRunning:        false,
		token:            token,
		groundControlURL: groundControlURL,
		mu:               &sync.Mutex{},
	}
}

type GroundControlConfigResponse struct {
	ID          int32         `json:"id"`
	ConfigName  string        `json:"config_name"`
	RegistryUrl string        `json:"registry_url"`
	Config      config.Config `json:"config"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
}

type GroundControlPayload struct {
	States []string `json:"states"`
}

type GroundControlConfigEvent struct {
	Name    string
	Payload GroundControlPayload
	Source  string
}

func NewGroundControlConfigEvent(states []string) scheduler.Event {
	return scheduler.Event{
		Name: FetchConfigFromGroundControlEventName,
		Payload: GroundControlPayload{
			States: states,
		},
		Source: config.UpdateConfigJobName,
	}
}

func (f *FetchConfigFromGroundControlProcess) Execute(ctx context.Context) error {
	log := logger.FromContext(ctx)
	if !f.start() {
		log.Warn().Msgf("Process %s is already running", f.name)
		return nil
	}
	defer f.stop()

	canExecute, reason := f.CanExecute(ctx)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", f.name, reason)
		return nil
	}

	log.Info().Msgf("Executing process %s", f.name)

	// Fetch configuration from Ground Control
	configData, err := f.fetchConfigFromGroundControl(ctx)
	if err != nil {
		log.Error().Msgf("Failed to fetch config from Ground Control: %v", err)
		return err
	}

	// Process the received configuration
	states, err := f.processConfig(configData, ctx)
	if err != nil {
		log.Error().Msgf("Failed to process config: %v", err)
		return err
	}

	// Publish the ground control config event
	groundControlConfigEvent := NewGroundControlConfigEvent(states)

	if err := f.eventBroker.Publish(groundControlConfigEvent, ctx); err != nil {
		log.Error().Msgf("Failed to publish ground control config event: %v", err)
		return fmt.Errorf("failed to publish ground control config event: %w", err)
	}

	log.Info().Msgf("Successfully fetched and processed config from Ground Control")
	return nil
}

func (f *FetchConfigFromGroundControlProcess) fetchConfigFromGroundControl(ctx context.Context) (*GroundControlConfigResponse, error) {
	// Construct the URL for fetching config
	// Based on your route: r.HandleFunc("/configs/{config}", s.getConfigHandler).Methods("GET")
	// We need to determine the config identifier - could be satellite ID, name, or token
	log := logger.FromContext(ctx)
	configURL := fmt.Sprintf("%s/%s/%s", f.groundControlURL, GroundControlSyncPath, f.GetName())
	log.Info().Msgf("Config-URL: %v", configURL)

	client := &http.Client{}

	// Create a new request for fetching configuration
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch config from Ground Control: %s", response.Status)
	}

	var configResponse GroundControlConfigResponse
	if err := json.NewDecoder(response.Body).Decode(&configResponse); err != nil {
		return nil, fmt.Errorf("failed to decode config response: %w", err)
	}
	// printFormattedConfig(configResponse, log)

	return &configResponse, nil
}

func (f *FetchConfigFromGroundControlProcess) processConfig(configData *GroundControlConfigResponse, ctx context.Context) ([]string, error) {
	log := logger.FromContext(ctx)

	// Extract states from the StateConfig
	var states []string

	// The primary state URL comes from StateConfig.StateURL
	if configData.Config.StateConfig.StateURL != "" {
		states = append(states, configData.Config.StateConfig.StateURL)
		log.Debug().Msgf("Found state URL in StateConfig: %s", configData.Config.StateConfig.StateURL)
	}

	// Log registry credentials info for debugging (without exposing sensitive data)
	stateAuth := configData.Config.StateConfig.RegistryCredentials
	if stateAuth.URL != "" {
		log.Debug().Msgf("State registry URL: %s", string(stateAuth.URL))
		log.Debug().Msgf("State registry has username: %t", stateAuth.Username != "")
		log.Debug().Msgf("State registry has password: %t", stateAuth.Password != "")
	}

	// Log app config info
	appConfig := configData.Config.AppConfig
	if appConfig.GroundControlURL != "" {
		log.Debug().Msgf("Ground Control URL in config: %s", string(appConfig.GroundControlURL))
	}
	if appConfig.StateReplicationInterval != "" {
		log.Debug().Msgf("State replication interval: %s", appConfig.StateReplicationInterval)
	}
	if appConfig.UpdateConfigInterval != "" {
		log.Debug().Msgf("Update config interval: %s", appConfig.UpdateConfigInterval)
	}

	// Log local registry info
	localRegistry := appConfig.LocalRegistryCredentials
	if localRegistry.URL != "" {
		log.Debug().Msgf("Local registry URL: %s", string(localRegistry.URL))
		log.Debug().Msgf("Local registry has credentials: %t", localRegistry.Username != "")
	}

	log.Info().Msgf("Processed config '%s' (ID: %d) with %d states from registry: %s",
		configData.ConfigName, configData.ID, len(states), configData.RegistryUrl)

	return states, nil
}
func (f *FetchConfigFromGroundControlProcess) GetID() cron.EntryID {
	return f.id
}

func (f *FetchConfigFromGroundControlProcess) SetID(id cron.EntryID) {
	f.id = id
}

func (f *FetchConfigFromGroundControlProcess) GetName() string {
	return f.name
}

func (f *FetchConfigFromGroundControlProcess) GetCronExpr() string {
	return f.cronExpr
}

func (f *FetchConfigFromGroundControlProcess) IsRunning() bool {
	return f.isRunning
}

func (f *FetchConfigFromGroundControlProcess) CanExecute(ctx context.Context) (bool, string) {
	log := logger.FromContext(ctx)
	log.Info().Msgf("Checking if process %s can execute", f.name)

	checks := []struct {
		condition bool
		message   string
	}{
		{f.token == "", "token"},
		{f.groundControlURL == "", "ground control URL"},
	}

	var missing []string
	for _, check := range checks {
		if check.condition {
			missing = append(missing, check.message)
		}
	}

	if len(missing) > 0 {
		return false, fmt.Sprintf("missing %s, please include the required environment variables", strings.Join(missing, ", "))
	}

	return true, fmt.Sprintf("Process %s can execute all conditions fulfilled", f.name)
}

func (f *FetchConfigFromGroundControlProcess) AddEventBroker(eventBroker *scheduler.EventBroker, ctx context.Context) {
	f.eventBroker = eventBroker
}

func (f *FetchConfigFromGroundControlProcess) start() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isRunning {
		return false
	}
	f.isRunning = true
	return true
}

func (f *FetchConfigFromGroundControlProcess) stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.isRunning = false
}
