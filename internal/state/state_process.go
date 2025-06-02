package state

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

type FetchAndReplicateStateProcess struct {
	name                string
	isRunning           bool
	stateMap            []StateMap
	currentConfigDigest string
	cm                  *config.ConfigManager
	mu                  sync.Mutex
}

func NewFetchAndReplicateStateProcess(cm *config.ConfigManager) *FetchAndReplicateStateProcess {
	return &FetchAndReplicateStateProcess{
		name:                config.ReplicateStateJobName,
		isRunning:           false,
		currentConfigDigest: "",
		cm:                  cm,
	}
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

func (f *FetchAndReplicateStateProcess) Execute(ctx context.Context) error {
	f.start()
	defer f.stop()
	log := logger.FromContext(ctx)

	sourceURL := utils.FormatRegistryURL(f.cm.GetSourceRegistryURL())
	remoteURL := utils.FormatRegistryURL(f.cm.GetRemoteRegistryURL())

	srcUsername := f.cm.GetSourceRegistryUsername()
	srcPassword := f.cm.GetSourceRegistryPassword()
	remoteUsername := f.cm.GetRemoteRegistryUsername()
	remotePassword := f.cm.GetRemoteRegistryPassword()

	useUnsecure := f.cm.UseUnsecure()
	satelliteStateURL := f.cm.GetStateURL()

	replicator := NewBasicReplicator(srcUsername, srcPassword, sourceURL, remoteURL, remoteUsername, remotePassword, useUnsecure)

	canExecute, reason := f.CanExecute(satelliteStateURL, remoteURL, sourceURL, srcUsername, srcPassword)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", f.name, reason)
		return nil
	}
	log.Info().Msg(reason)

	satelliteStateFetcher, err := getStateFetcherForInput(satelliteStateURL, srcUsername, srcPassword, useUnsecure, log)
	if err != nil {
		log.Error().Err(err).Msg("Error processing satellite state")
		return err
	}
	satelliteState := &SatelliteState{}
	if err := satelliteStateFetcher.FetchStateArtifact(ctx, satelliteState, log); err != nil {
		log.Error().Err(err).Msgf("Error fetching state artifact from url: %s", satelliteStateURL)
		return err
	}

	f.updateStateMap(satelliteState.States)

	var g errgroup.Group
	mutex := &sync.Mutex{}
	errMutex := &sync.Mutex{}
	var allErrors []string

	// State fetching
	for i := range f.stateMap {
		i := i
		g.Go(func() error {
			log.Info().Msgf("Processing state for %s", f.stateMap[i].url)
			groupStateFetcher, err := getStateFetcherForInput(f.stateMap[i].url, srcUsername, srcPassword, useUnsecure, log)
			if err != nil {
				errMutex.Lock()
				allErrors = append(allErrors, err.Error())
				errMutex.Unlock()
				log.Error().Err(err).Msg("Error processing input")
				return nil
			}

			newStateFetched, err := f.FetchAndProcessState(ctx, groupStateFetcher, log)
			if err != nil {
				errMutex.Lock()
				allErrors = append(allErrors, err.Error())
				errMutex.Unlock()
				log.Error().Err(err).Msg("Error fetching state")
				return nil
			}
			log.Info().Msgf("State fetched successfully for %s", f.stateMap[i].url)

			deleteEntity, replicateEntity, newState := f.GetChanges(*newStateFetched, log, f.stateMap[i].Entities)
			f.LogChanges(deleteEntity, replicateEntity, log)

			if err := replicator.DeleteReplicationEntity(ctx, deleteEntity); err != nil {
				errMutex.Lock()
				allErrors = append(allErrors, err.Error())
				errMutex.Unlock()
				log.Error().Err(err).Msg("Error deleting entities")
				return nil
			}
			if err := replicator.Replicate(ctx, replicateEntity); err != nil {
				errMutex.Lock()
				allErrors = append(allErrors, err.Error())
				errMutex.Unlock()
				log.Error().Err(err).Msg("Error replicating state")
				return nil
			}

			mutex.Lock()
			f.stateMap[i].State = newState
			f.stateMap[i].Entities = FetchEntitiesFromState(newState)
			mutex.Unlock()

			return nil
		})
	}

	// Config fetching in a separate goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		configStateFetcher, err := getStateFetcherForInput(satelliteState.Config, srcUsername, srcPassword, useUnsecure, log)
		if err != nil {
			errMutex.Lock()
			allErrors = append(allErrors, err.Error())
			errMutex.Unlock()
			log.Error().Err(err).Msg("Error processing satellite state")
			return
		}

		configDigest, err := configStateFetcher.FetchDigest(ctx, log)
		if err != nil {
			errMutex.Lock()
			allErrors = append(allErrors, err.Error())
			errMutex.Unlock()
			log.Error().Err(err).Msgf("Error fetching state artifact digest from url: %s", satelliteState.Config)
			return
		}

		if configDigest != f.currentConfigDigest {
			log.Info().Str("Current Digest", f.currentConfigDigest).Str("Remote Digest", configDigest).Msgf("The upstream config has changes, reconciling the satellite accordingly")
			remoteConfig := config.Config{}
			if err := configStateFetcher.FetchStateArtifact(ctx, &remoteConfig, log); err != nil {
				log.Error().Err(err).
					Msgf("Error fetching new config's state artifact from url: %s, continuing execution with the previous config with digest %s", satelliteState.Config, f.currentConfigDigest)
				return
			}
			remoteConfig.StateConfig = f.cm.GetStateConfig()
			validatedRemoteConfig, warnings, err := config.ValidateAndEnforceDefaults(&remoteConfig, f.cm.DefaultGroundControlURL)
			if err != nil {
				log.Error().Err(err).
					Msgf("Error validating config state artifact digest from url: %s, continuing execution with the previous config with digest %s", satelliteState.Config, f.currentConfigDigest)
				return
			}
			if len(warnings) != 0 {
				utils.HandleNewConfigWarnings(log, warnings)
			}
			if err := f.cm.WritePrevConfigToDisk(f.cm.GetConfig()); err != nil {
				log.Error().Err(err).
					Msgf("Error writing the prev config to disk while reconciling remote config, continuing execution with the same previous config with digest %s", f.currentConfigDigest)
				return
			}
			log.Debug().Str("Current Digest", f.currentConfigDigest).Str("Remote Digest", configDigest).Msgf("Writing new config to disk")
			if err := f.cm.WriteConfigToDisk(validatedRemoteConfig); err != nil {
				log.Error().Err(err).
					Msgf("Error writing the newly fetched remote config from %s to disk, continuing execution with the previous config with digest %s", satelliteState.Config, f.currentConfigDigest)
				return
			}
			f.currentConfigDigest = configDigest
		}
	}()

    // No need to check the error here, as we accumulate all the errors in the end.
	_ = g.Wait()
	<-done

	if len(allErrors) > 0 {
		return fmt.Errorf("the following errors occurred while reconciling satellite state: %s", strings.Join(allErrors, "; "))
	}

	return nil
}
func (f *FetchAndReplicateStateProcess) updateStateMap(states []string) {
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

func (f *FetchAndReplicateStateProcess) GetChanges(newState StateReader, log *zerolog.Logger, oldEntites []Entity) ([]Entity, []Entity, StateReader) {
	log.Info().Msg("Getting changes")
	newState = f.RemoveNullTagArtifacts(newState)
	newEntites := FetchEntitiesFromState(newState)

	var entityToDelete []Entity
	var entityToReplicate []Entity

	if oldEntites == nil {
		log.Warn().Msg("Old state has zero entites, replicating the complete state")
		return entityToDelete, newEntites, newState
	}

	oldEntityMap := make(map[string]Entity)
	for _, oldEntity := range oldEntites {
		key := oldEntity.Name + "|" + oldEntity.Tag
		oldEntityMap[key] = oldEntity
		log.Debug().Str("entity", key).Str("digest", oldEntity.Digest).Msg("Added old entity to lookup map")
	}

	for _, newEntity := range newEntites {
		key := newEntity.Name + "|" + newEntity.Tag
		oldEntity, exists := oldEntityMap[key]

		if !exists {
			log.Debug().Str("entity", key).Msg("New entity not found in old state, scheduling for replication")
			entityToReplicate = append(entityToReplicate, newEntity)
		} else if newEntity.Digest != oldEntity.Digest {
			log.Debug().Str("entity", key).
				Str("old_digest", oldEntity.Digest).
				Str("new_digest", newEntity.Digest).
				Msg("Entity digest changed, scheduling old for delete and new for replicate")
			entityToReplicate = append(entityToReplicate, newEntity)
			entityToDelete = append(entityToDelete, oldEntity)
		} else {
			log.Debug().Str("entity", key).Msg("Entity unchanged, skipping")
		}
		delete(oldEntityMap, key)
	}

	for _, oldEntity := range oldEntityMap {
		key := oldEntity.Name + "|" + oldEntity.Tag
		log.Debug().Str("entity", key).Msg("Old entity no longer present, scheduling for deletion")
		entityToDelete = append(entityToDelete, oldEntity)
	}

	return entityToDelete, entityToReplicate, newState
}

func (f *FetchAndReplicateStateProcess) IsRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isRunning
}

func (f *FetchAndReplicateStateProcess) Name() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name
}

// The state fetch process is prepetual, the only criteria for completion is
// if the statellite is shut down.
func (f *FetchAndReplicateStateProcess) IsComplete() bool {
	return false
}

func (f *FetchAndReplicateStateProcess) CanExecute(satelliteStateURL, remoteURL, srcURL, srcUsername, srcPassword string) (bool, string) {
	checks := []struct {
		condition bool
		message   string
	}{
		{satelliteStateURL == "", "satelliteState is empty"},
		{remoteURL == "", "remote registry URL is empty"},
		{srcUsername == "", "username is empty"},
		{srcURL == "", "source registry is empty"},
		{srcPassword == "", "password is empty"},
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

func (f *FetchAndReplicateStateProcess) start() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.isRunning = true
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

func (f *FetchAndReplicateStateProcess) FetchAndProcessState(ctx context.Context, fetcher StateFetcher, log *zerolog.Logger) (*StateReader, error) {
	state := NewState()
	err := fetcher.FetchStateArtifact(ctx, state, log)
	if err != nil {
		log.Error().Err(err).Msg("Error fetching state artifact")
		return nil, err
	}
	// update this function to now fetch the list of states from earlier
	return ProcessState(&state)
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

// contains takes in a slice and checks if the item is in the slice if preset it returns true else false
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
