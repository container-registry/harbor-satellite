package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/robfig/cron/v3"
)

const FetchConfigFromGroundControlEventName string = "fetch-config-from-ground-control-event"

const GroundControlSyncPath string = "/satellites/%s/sync"

type FetchConfigFromGroundControlProcess struct {
	id               cron.EntryID
	name             string
	cronExpr         string
	isRunning        bool
	token            string
	groundControlURL string
	mu               *sync.Mutex
	eventBroker      *scheduler.EventBroker
	satelliteName    string
}

func NewFetchConfigFromGroundControlProcess(cronExpr string, token string, groundControlURL string, satelliteName string) *FetchConfigFromGroundControlProcess {
	return &FetchConfigFromGroundControlProcess{
		name:             config.UpdateConfigJobName,
		cronExpr:         cronExpr,
		isRunning:        false,
		token:            token,
		groundControlURL: groundControlURL,
		mu:               &sync.Mutex{},
		satelliteName:    satelliteName,
	}
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

	client := &http.Client{}

	satelliteName := config.GetSatelliteName()
	if satelliteName == "" {
		return fmt.Errorf("satellite name not configured")
	}

	syncPath := fmt.Sprintf(GroundControlSyncPath, satelliteName)
	log.Info().Msgf("Fetching config from Ground Control: %s", f.groundControlURL+syncPath)
	req, err := http.NewRequestWithContext(ctx, "GET", f.groundControlURL+syncPath, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	var payload GroundControlPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	log.Info().Msgf("Received %d states from Ground Control", len(payload.States))

	if err := config.UpdateStates(payload.States); err != nil {
		return fmt.Errorf("failed to update states in config: %v", err)
	}

	event := NewGroundControlConfigEvent(payload.States)
	if f.eventBroker != nil {
		f.eventBroker.Publish(event, ctx)
	}

	return nil
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
	return false, fmt.Sprintf("Process %s can execute all condition fulfilled", f.name)
}

func (f *FetchConfigFromGroundControlProcess) AddEventBroker(eventBroker *scheduler.EventBroker, ctx context.Context) {
	f.eventBroker = eventBroker
}

// comment out unused functions to ignore linter warnings. nolint isn't working for some reason.

//func (f *FetchConfigFromGroundControlProcess) start() bool {
//	f.mu.Lock()
//	defer f.mu.Unlock()
//	if f.isRunning {
//	return false
//}
//f.isRunning = true
//return true
//}

//func (f *FetchConfigFromGroundControlProcess) stop() {
//f.mu.Lock()
//defer f.mu.Unlock()
//f.isRunning = false
//}
