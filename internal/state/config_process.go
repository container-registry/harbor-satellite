package state

import (
	"context"
	"fmt"
	"sync"

	"container-registry.com/harbor-satellite/internal/scheduler"
	"container-registry.com/harbor-satellite/logger"
	"github.com/robfig/cron/v3"
)

const FetchConfigFromGroundControlProcessName string = "fetch-config-from-ground-control-process"

const DefaultFetchConfigFromGroundControlTimePeriod string = "00h00m030s"

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
		name:             FetchConfigFromGroundControlProcessName,
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
		Source: FetchConfigFromGroundControlProcessName,
	}
}

func (f *FetchConfigFromGroundControlProcess) Execute(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msgf("Starting process %s", f.name)
	if !f.start() {
		log.Warn().Msg("Process is already running")
		return nil
	}
	defer f.stop()
	log.Info().Msg("Fetching config from ground control")
	event := NewGroundControlConfigEvent([]string{"state1", "state2"})
	f.eventBroker.Publish(event, ctx)
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
	return fmt.Sprintf("@every %s", f.cronExpr)
}

func (f *FetchConfigFromGroundControlProcess) IsRunning() bool {
	return f.isRunning
}

func (f *FetchConfigFromGroundControlProcess) CanExecute(ctx context.Context) (bool, string) {
	return true, fmt.Sprintf("Process %s can execute all condition fulfilled", f.name)
}

func (f *FetchConfigFromGroundControlProcess) AddEventBroker(eventBroker *scheduler.EventBroker, ctx context.Context) {
	log := logger.FromContext(ctx)
	log.Info().Msgf("Adding event broker to process %s", f.name)
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
