package state

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/notifier"
	"container-registry.com/harbor-satellite/internal/scheduler"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

type FetchAndReplicateAuthConfig struct {
	SourceRegistry         string
	SourceRegistryUserName string
	SourceRegistryPassword string
	UseUnsecure            bool
	RemoteRegistryURL      string
	RemoteRegistryUserName string
	RemoteRegistryPassword string
}

type FetchAndReplicateStateProcess struct {
	id          cron.EntryID
	name        string
	cronExpr    string
	isRunning   bool
	stateMap    []StateMap
	notifier    notifier.Notifier
	mu          *sync.Mutex
	authConfig  FetchAndReplicateAuthConfig
	eventBroker *scheduler.EventBroker
	Replicator  Replicator
}

type StateMap struct {
	url      string
	State    StateReader
	Entities []Entity
}

func NewStateMap(url []string) []StateMap {
	var stateMap []StateMap
	for _, u := range url {
		stateMap = append(stateMap, StateMap{url: u, State: nil, Entities: nil})
	}
	return stateMap
}

func NewFetchAndReplicateStateProcess(cronExpr string, notifier notifier.Notifier, sourceRegistryURL, sourceRegistryUsername, sourceRegistryPassword, remoteRegistryURL, remoteRegistryUsername, remoteRegistryPassword string, useUnsecure bool, states []string) *FetchAndReplicateStateProcess {
	sourceURL := utils.FormatRegistryURL(sourceRegistryURL)
	remoteURL := utils.FormatRegistryURL(remoteRegistryURL)
	return &FetchAndReplicateStateProcess{
		name:      config.ReplicateStateJobName,
		cronExpr:  cronExpr,
		isRunning: false,
		notifier:  notifier,
		mu:        &sync.Mutex{},
		stateMap:  NewStateMap(states),
		authConfig: FetchAndReplicateAuthConfig{
			SourceRegistry:         sourceURL,
			SourceRegistryUserName: sourceRegistryUsername,
			SourceRegistryPassword: sourceRegistryPassword,
			UseUnsecure:            useUnsecure,
			RemoteRegistryURL:      remoteURL,
			RemoteRegistryUserName: remoteRegistryUsername,
			RemoteRegistryPassword: remoteRegistryPassword,
		},
		Replicator: NewBasicReplicator(sourceRegistryUsername, sourceRegistryPassword, sourceURL, remoteURL, remoteRegistryUsername, remoteRegistryPassword, useUnsecure),
	}
}

func (f *FetchAndReplicateStateProcess) Execute(ctx context.Context) error {
	defer f.stop()
	log := logger.FromContext(ctx)
	if !f.start() {
		log.Warn().Msgf("Process %s is already running", f.name)
		return nil
	}
	canExecute, reason := f.CanExecute(ctx)
	if !canExecute {
		log.Warn().Msgf("Cannot execute process: %s", reason)
		return nil
	}
	log.Info().Msg(reason)

	for i := range f.stateMap {
		log.Info().Msgf("Processing state for %s", f.stateMap[i].url)
		stateFetcher, err := processInput(f.stateMap[i].url, f.authConfig.SourceRegistryUserName, f.authConfig.SourceRegistryPassword, log)
		if err != nil {
			log.Error().Err(err).Msg("Error processing input")
			return err
		}
		newStateFetched, err := f.FetchAndProcessState(stateFetcher, log)
		if err != nil {
			log.Error().Err(err).Msg("Error fetching state")
			return err
		}
		log.Info().Msgf("State fetched successfully for %s", f.stateMap[i].url)
		deleteEntity, replicateEntity, newState := f.GetChanges(newStateFetched, log, f.stateMap[i].Entities)
		f.LogChanges(deleteEntity, replicateEntity, log)
		if err := f.notifier.Notify(); err != nil {
			log.Error().Err(err).Msg("Error sending notification")
		}
		// Delete the entities from the remote registry
		if err := f.Replicator.DeleteReplicationEntity(ctx, deleteEntity); err != nil {
			log.Error().Err(err).Msg("Error deleting entities")
			return err
		}
		// Replicate the entities to the remote registry
		if err := f.Replicator.Replicate(ctx, replicateEntity); err != nil {
			log.Error().Err(err).Msg("Error replicating state")
			return err
		}
		// Update the state directly in the slice
		f.stateMap[i].State = newState
		f.stateMap[i].Entities = FetchEntitiesFromState(newState)
	}
	return nil
}

func (f *FetchAndReplicateStateProcess) GetChanges(newState StateReader, log *zerolog.Logger, oldEntites []Entity) ([]Entity, []Entity, StateReader) {
	log.Info().Msg("Getting changes")
	// Remove artifacts with null tags from the new state
	newState = f.RemoveNullTagArtifacts(newState)
	newEntites := FetchEntitiesFromState(newState)

	var entityToDelete []Entity
	var entityToReplicate []Entity

	if oldEntites == nil {
		log.Warn().Msg("Old state has zero entites, replicating the complete state")
		return entityToDelete, newEntites, newState
	}

	// Create maps for quick lookups
	oldEntityMap := make(map[string]Entity)
	for _, oldEntity := range oldEntites {
		oldEntityMap[oldEntity.Name+"|"+oldEntity.Tag] = oldEntity
	}

	// Check new artifacts and update lists
	for _, newEntity := range newEntites {
		nameTagKey := newEntity.Name + "|" + newEntity.Tag
		oldEntity, exists := oldEntityMap[nameTagKey]

		if !exists {
			// New artifact doesn't exist in old state, add to replication list
			entityToReplicate = append(entityToReplicate, newEntity)
		} else if newEntity.Digest != oldEntity.Digest {
			// Artifact exists but has changed, add to both lists
			entityToReplicate = append(entityToReplicate, newEntity)
			entityToDelete = append(entityToDelete, oldEntity)
		}

		// Remove processed old artifact from map
		delete(oldEntityMap, nameTagKey)
	}

	// Remaining artifacts in oldArtifactsMap should be deleted
	for _, oldEntity := range oldEntityMap {
		entityToDelete = append(entityToDelete, oldEntity)
	}

	return entityToDelete, entityToReplicate, newState
}
func (f *FetchAndReplicateStateProcess) GetID() cron.EntryID {
	return f.id
}

func (f *FetchAndReplicateStateProcess) SetID(id cron.EntryID) {
	f.id = id
}

func (f *FetchAndReplicateStateProcess) GetName() string {
	return f.name
}

func (f *FetchAndReplicateStateProcess) GetCronExpr() string {
	return f.cronExpr
}

func (f *FetchAndReplicateStateProcess) IsRunning() bool {
	return f.isRunning
}

func (f *FetchAndReplicateStateProcess) CanExecute(ctx context.Context) (bool, string) {
	checks := []struct {
		condition bool
		message   string
	}{
		{f.stateMap == nil, "state map is nil"},
		{f.authConfig.RemoteRegistryURL == "", "remote registry URL is empty"},
		{f.authConfig.SourceRegistry == "", "source registry is empty"},
		{f.authConfig.SourceRegistryUserName == "", "username is empty"},
		{f.authConfig.SourceRegistryPassword == "", "password is empty"},
	}

	var missingFields []string
	for _, check := range checks {
		if check.condition {
			missingFields = append(missingFields, check.message)
		}
	}

	if len(missingFields) > 0 {
		return false, fmt.Sprintf("missing %s", strings.Join(missingFields, ", "))
	}

	return true, fmt.Sprintf("Process %s can execute: all conditions fulfilled", f.name)
}

func (f *FetchAndReplicateStateProcess) start() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isRunning {
		return false
	}
	f.isRunning = true
	return true
}

func (f *FetchAndReplicateStateProcess) stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.isRunning = false
}

func (f *FetchAndReplicateStateProcess) RemoveNullTagArtifacts(state StateReader) StateReader {
	var artifactsWithoutNullTags []ArtifactReader
	for _, artifact := range state.GetArtifacts() {
		if artifact.GetTags() != nil && len(artifact.GetTags()) != 0 {
			artifactsWithoutNullTags = append(artifactsWithoutNullTags, artifact)
		}
	}
	state.SetArtifacts(artifactsWithoutNullTags)
	return state
}

func ProcessState(state *StateReader) (*StateReader, error) {
	for _, artifact := range (*state).GetArtifacts() {
		repo, image, err := utils.GetRepositoryAndImageNameFromArtifact(artifact.GetRepository())
		if err != nil {
			fmt.Printf("Error in getting repository and image name: %v", err)
			return nil, err
		}
		artifact.SetRepository(repo)
		artifact.SetName(image)
	}
	return state, nil
}

func (f *FetchAndReplicateStateProcess) FetchAndProcessState(fetcher StateFetcher, log *zerolog.Logger) (StateReader, error) {
	state := NewState()
	err := fetcher.FetchStateArtifact(&state)
	if err != nil {
		log.Error().Err(err).Msg("Error fetching state artifact")
		return nil, err
	}
	ProcessState(&state)
	return state, nil
}

func (f *FetchAndReplicateStateProcess) LogChanges(deleteEntity, replicateEntity []Entity, log *zerolog.Logger) {
	log.Warn().Msgf("Total artifacts to delete: %d", len(deleteEntity))
	log.Warn().Msgf("Total artifacts to replicate: %d", len(replicateEntity))
}

func FetchEntitiesFromState(state StateReader) []Entity {
	var entities []Entity
	for _, artifact := range state.GetArtifacts() {
		for _, tag := range artifact.GetTags() {
			entities = append(entities, Entity{
				Name:       artifact.GetName(),
				Repository: artifact.GetRepository(),
				Tag:        tag,
				Digest:     artifact.GetDigest(),
			})
		}
	}
	return entities
}

func (f *FetchAndReplicateStateProcess) AddEventBroker(eventBroker *scheduler.EventBroker, ctx context.Context) {
	f.eventBroker = eventBroker
	go f.ListenForUpdatedConfig(ctx)
}

func (f *FetchAndReplicateStateProcess) ListenForUpdatedConfig(ctx context.Context) {
	log := logger.FromContext(ctx)
	log.Info().Msgf("Process %s is listening for updated config", f.name)
	fetchConfigCh := f.eventBroker.Subscribe(FetchConfigFromGroundControlEventName)
	zeroTouchRegistrationCh := f.eventBroker.Subscribe(ZeroTouchRegistrationEventName)

	defer func() {
		log.Info().Msgf("Process %s unsubscribing from %s and %s", f.name, FetchConfigFromGroundControlEventName, ZeroTouchRegistrationEventName)
		f.eventBroker.Unsubscribe(FetchConfigFromGroundControlEventName, fetchConfigCh)
		f.eventBroker.Unsubscribe(ZeroTouchRegistrationEventName, zeroTouchRegistrationCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-fetchConfigCh:
			log.Info().Msgf("Received updated config from ground control from source %s", event.Source)
		case event := <-zeroTouchRegistrationCh:
			f.HandelPayloadFromZTR(event, log)
		}
	}
}

func (f *FetchAndReplicateStateProcess) HandelPayloadFromZTR(event scheduler.Event, log *zerolog.Logger) {
	log.Info().Msgf("Received %s event with source %s", event.Name, event.Source)
	payload, ok := event.Payload.(ZeroTouchRegistrationEventPayload)
	if !ok {
		log.Error().Msgf("Received invalid payload from %s, for process %s", event.Source, ZeroTouchRegistrationEventName)
		return
	}
	f.UpdateFetchProcessConfigFromZtr(payload.StateConfig.Auth.SourceUsername, payload.StateConfig.Auth.SourcePassword, payload.StateConfig.Auth.Registry, payload.StateConfig.States)
}

func (f *FetchAndReplicateStateProcess) UpdateFetchProcessConfigFromZtr(username, password, sourceRegistryURL string, states []string) {
	f.authConfig.SourceRegistryUserName = username
	f.authConfig.SourceRegistryPassword = password
	f.authConfig.SourceRegistry = utils.FormatRegistryURL(sourceRegistryURL)

	// The states contain all the states that this satellite needs to track thus we would have to add the new states to the state map
	// also we would have to remove the states that are not in the new states
	var newStates []string
	for _, state := range states {
		found := false
		for _, stateMap := range f.stateMap {
			if stateMap.url == state {
				found = true
				break
			}
		}
		if !found {
			newStates = append(newStates, state)
		}
	}

	// Remove states that are no longer needed
	var updatedStateMap []StateMap
	for _, stateMap := range f.stateMap {
		if contains(states, stateMap.url) {
			updatedStateMap = append(updatedStateMap, stateMap)
		}
	}

	// Add new states
	f.stateMap = append(updatedStateMap, NewStateMap(newStates)...)
}

// contains takes in a slice and checks if the item is in the slice if preset it returns true else false
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
