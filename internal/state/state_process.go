package state

import (
	"context"
	"fmt"
	"sync"

	"container-registry.com/harbor-satellite/internal/notifier"
	"container-registry.com/harbor-satellite/logger"
)

const FetchAndReplicateStateProcessName string = "fetch-replicate-state-process"

const DefaultFetchAndReplicateStateTimePeriod string = "00h00m05s"

type FetchAndReplicateStateProcess struct {
	id                   uint64
	name                 string
	stateArtifactFetcher StateFetcher
	cronExpr             string
	isRunning            bool
	stateReader          StateReader
	notifier             notifier.Notifier
	mu                   *sync.Mutex
}

func NewFetchAndReplicateStateProcess(id uint64, cronExpr string, stateFetcher StateFetcher, notifier notifier.Notifier) *FetchAndReplicateStateProcess {
	return &FetchAndReplicateStateProcess{
		id:                   id,
		name:                 FetchAndReplicateStateProcessName,
		cronExpr:             cronExpr,
		isRunning:            false,
		stateArtifactFetcher: stateFetcher,
		notifier:             notifier,
		mu:                   &sync.Mutex{},
	}
}

func (f *FetchAndReplicateStateProcess) Execute(ctx context.Context) error {
	log := logger.FromContext(ctx)
	f.mu.Lock()
	if f.IsRunning() {
		f.mu.Unlock()
		log.Warn().Msg("Process is already running")
		return fmt.Errorf("process %s is already running", f.GetName())
	}
	log.Info().Msg("Starting process to fetch and replicate state")
	f.isRunning = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.isRunning = false
		f.mu.Unlock()
	}()

	newStateFetched, err := f.stateArtifactFetcher.FetchStateArtifact()
	if err != nil {
		log.Error().Err(err).Msg("Error fetching state artifact")
		return err
	}
	if !f.HasStateChanged(newStateFetched) {
		log.Info().Msg("State has not changed")
		return nil
	}
	if err := f.notifier.Notify(); err != nil {
		log.Error().Err(err).Msg("Error sending notification")
	}

	replicator := NewBasicReplicator(newStateFetched)
	if err := replicator.Replicate(ctx); err != nil {
		log.Error().Err(err).Msg("Error replicating state")
		return err
	}
	f.stateReader = newStateFetched
	return nil
}

func (f *FetchAndReplicateStateProcess) HasStateChanged(newState StateReader) bool {
	if f.stateReader == nil {
		return true
	}
	return f.stateReader.HasStateChanged(newState)
}

func (f *FetchAndReplicateStateProcess) GetID() uint64 {
	return f.id
}

func (f *FetchAndReplicateStateProcess) GetName() string {
	return f.name
}

func (f *FetchAndReplicateStateProcess) GetCronExpr() string {
	return fmt.Sprintf("@every %s", f.cronExpr)
}

func (f *FetchAndReplicateStateProcess) IsRunning() bool {
	return f.isRunning
}
