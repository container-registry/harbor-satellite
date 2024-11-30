package state

import (
	"context"
	"fmt"
	"sync"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/scheduler"
	"github.com/robfig/cron/v3"
)

const FetchConfigFromGroundControlEventName string = "fetch-config-from-ground-control-event"

const GroundControlSyncPath string = "/satellites/sync"

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
	// TODO: Implement the logic to fetch the configuration from Ground Control one the endpoint is available on the Ground Control side
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
