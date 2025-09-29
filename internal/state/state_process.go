package state

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

type FetchAndReplicateStateProcess struct {
	name                string
	isRunning           bool
	stateMap            []StateMap
	currentConfigDigest string
	cm                  *config.ConfigManager
	mu                  sync.Mutex
}

// Define result types for channels
type StateFetcherResult struct {
	Index     int
	URL       string
	Error     error
	Cancelled bool
}

type ConfigFetcherResult struct {
	ConfigDigest string
	Error        error
	Cancelled    bool
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

func (f *FetchAndReplicateStateProcess) Execute(ctx context.Context, upstreamPayload chan scheduler.UpstreamInfo) error {

	// the payload that will be populated with all relevant information during this process's execution
	payload := scheduler.UpstreamInfo{}

	defer func() {
		if upstreamPayload != nil {
			upstreamPayload <- payload
		}
	}()

	f.start()
	defer f.stop()

	// Top level logger with process name
	log := logger.FromContext(ctx).With().Str("process", f.name).Logger()

	// Check for early cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	sourceURL := utils.FormatRegistryURL(f.cm.GetSourceRegistryURL())
	remoteURL := utils.FormatRegistryURL(f.cm.GetRemoteRegistryURL())
	srcUsername := f.cm.GetSourceRegistryUsername()
	srcPassword := f.cm.GetSourceRegistryPassword()
	remoteUsername := f.cm.GetRemoteRegistryUsername()
	remotePassword := f.cm.GetRemoteRegistryPassword()
	useUnsecure := f.cm.UseUnsecure()
	satelliteStateURL := f.cm.GetStateURL()

	payload.StateURL = satelliteStateURL

	replicator := NewBasicReplicator(srcUsername, srcPassword, sourceURL, remoteURL, remoteUsername, remotePassword, useUnsecure)

	canExecute, reason := f.CanExecute(satelliteStateURL, remoteURL, sourceURL, srcUsername, srcPassword)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", f.name, reason)
		return nil
	}
	log.Info().Msg(reason)

	satelliteStateFetcher, err := getStateFetcherForInput(satelliteStateURL, srcUsername, srcPassword, useUnsecure, &log)
	if err != nil {
		log.Error().Err(err).Msg("Error processing satellite state")
		payload.CurrentActivity = "encountered error"
		return err
	}
	satelliteState := &SatelliteState{}
	if err := satelliteStateFetcher.FetchStateArtifact(ctx, satelliteState, &log); err != nil {
		log.Error().Err(err).Msgf("Error fetching state artifact from url: %s", satelliteStateURL)
		payload.CurrentActivity = "encountered error"
		return err
	}

	// fetch digest of latest state and put into the payload
	stateDigest, err := satelliteStateFetcher.FetchDigest(ctx, &log)
	if err != nil {
		log.Error().Err(err).Msgf("Error fetching digest from latest state artifact")
		payload.CurrentActivity = "encountered error"
	}

	if stateDigest != "" {
		payload.LatestStateDigest = stateDigest
	}

	f.updateStateMap(satelliteState.States)

	// Create channels for results
	stateFetcherResults := make(chan StateFetcherResult, len(f.stateMap))
	configFetcherResult := make(chan ConfigFetcherResult, 1)

	// Mutex for concurrency safe access of the stateMap
	mutex := &sync.Mutex{}

	// Launch state fetcher goroutines
	for i := range f.stateMap {
		go func(index int) {
			stateFetcherLog := log.With().
				Str("sub-process", "state-fetcher").
				Str("group", f.stateMap[index].url).
				Int("goroutine-id", index).
				Logger()

			result := StateFetcherResult{
				Index: index,
				URL:   f.stateMap[index].url,
			}

			stateFetcherLog.Info().Msgf("Processing state for %s", f.stateMap[index].url)

			groupStateFetcher, err := getStateFetcherForInput(f.stateMap[index].url, srcUsername, srcPassword, useUnsecure, &stateFetcherLog)
			if err != nil {
				stateFetcherLog.Error().Err(err).Msg("Error processing input")
				result.Error = fmt.Errorf("failed to create state fetcher for %s: %w", f.stateMap[index].url, err)
				stateFetcherResults <- result
				payload.CurrentActivity = "encountered error"
				return
			}

			newStateFetched, err := f.FetchAndProcessState(ctx, groupStateFetcher, &stateFetcherLog)
			if err != nil {
				stateFetcherLog.Error().Err(err).Msg("Error fetching state")
				result.Error = fmt.Errorf("failed to fetch state for %s: %w", f.stateMap[index].url, err)
				stateFetcherResults <- result
				payload.CurrentActivity = "encountered error"
				return
			}
			stateFetcherLog.Info().Msgf("State fetched successfully for %s", f.stateMap[index].url)

			deleteEntity, replicateEntity, newState := f.GetChanges(*newStateFetched, &stateFetcherLog, f.stateMap[index].Entities)
			f.LogChanges(deleteEntity, replicateEntity, &stateFetcherLog)

			if err := replicator.DeleteReplicationEntity(ctx, deleteEntity); err != nil {
				stateFetcherLog.Error().Err(err).Msg("Error deleting entities")
				result.Error = fmt.Errorf("failed to delete entities for %s: %w", f.stateMap[index].url, err)
				stateFetcherResults <- result
				payload.CurrentActivity = "encountered error"
				return
			}

			if err := replicator.Replicate(ctx, replicateEntity); err != nil {
				stateFetcherLog.Error().Err(err).Msg("Error replicating state")
				result.Error = fmt.Errorf("failed to replicate entities for %s: %w", f.stateMap[index].url, err)
				stateFetcherResults <- result
				payload.CurrentActivity = "encountered error"
				return
			}

			mutex.Lock()
			f.stateMap[index].State = newState
			f.stateMap[index].Entities = FetchEntitiesFromState(newState)
			mutex.Unlock()

			stateFetcherResults <- result
		}(i)
	}

	// Launch config fetcher goroutine
	go func() {
		configFetcherLog := log.With().
			Str("sub-process", "config-fetcher").
			Logger()

		result := ConfigFetcherResult{}

		configStateFetcher, err := getStateFetcherForInput(satelliteState.Config, srcUsername, srcPassword, useUnsecure, &configFetcherLog)
		if err != nil {
			configFetcherLog.Error().Err(err).Msg("Error processing satellite state")
			result.Error = fmt.Errorf("failed to create config state fetcher: %w", err)
			configFetcherResult <- result
			payload.CurrentActivity = "encountered error"
			return
		}

		configDigest, err := configStateFetcher.FetchDigest(ctx, &configFetcherLog)
		payload.LatestConfigDigest = configDigest

		if err != nil {
			configFetcherLog.Error().Err(err).Msgf("Error fetching state artifact digest from url: %s", satelliteState.Config)
			result.Error = fmt.Errorf("failed to fetch config digest from %s: %w", satelliteState.Config, err)
			configFetcherResult <- result
			payload.CurrentActivity = "encountered error"
			return
		}

		if configDigest != f.currentConfigDigest {
			payload.CurrentActivity = "reconciling state"
			configFetcherLog.Info().Str("Current Digest", f.currentConfigDigest).Str("Remote Digest", configDigest).Msgf("The upstream config has changes, reconciling the satellite accordingly")

			remoteConfig := config.Config{}
			if err := configStateFetcher.FetchStateArtifact(ctx, &remoteConfig, &configFetcherLog); err != nil {
				configFetcherLog.Error().Err(err).
					Msgf("Error fetching new config's state artifact from url: %s, continuing execution with the previous config with digest %s", satelliteState.Config, f.currentConfigDigest)
				result.Error = fmt.Errorf("failed to fetch config artifact from %s: %w", satelliteState.Config, err)
				configFetcherResult <- result
				return
			}

			remoteConfig.StateConfig = f.cm.GetStateConfig()
			validatedRemoteConfig, warnings, err := config.ValidateAndEnforceDefaults(&remoteConfig, f.cm.DefaultGroundControlURL)
			if err != nil {
				configFetcherLog.Error().Err(err).
					Msgf("Error validating config state artifact digest from url: %s, continuing execution with the previous config with digest %s", satelliteState.Config, f.currentConfigDigest)
				result.Error = fmt.Errorf("failed to validate config from %s: %w", satelliteState.Config, err)
				configFetcherResult <- result
				return
			}
			if len(warnings) != 0 {
				utils.HandleNewConfigWarnings(&configFetcherLog, warnings)
			}

			if err := f.cm.WritePrevConfigToDisk(f.cm.GetConfig()); err != nil {
				configFetcherLog.Error().Err(err).
					Msgf("Error writing the prev config to disk while reconciling remote config, continuing execution with the same previous config with digest %s", f.currentConfigDigest)
				result.Error = fmt.Errorf("failed to write previous config to disk: %w", err)
				configFetcherResult <- result
				return
			}

			configFetcherLog.Debug().Str("Current Digest", f.currentConfigDigest).Str("Remote Digest", configDigest).Msgf("Writing new config to disk")
			if err := f.cm.WriteConfigToDisk(validatedRemoteConfig); err != nil {
				configFetcherLog.Error().Err(err).
					Msgf("Error writing the newly fetched remote config from %s to disk, continuing execution with the previous config with digest %s", satelliteState.Config, f.currentConfigDigest)
				result.Error = fmt.Errorf("failed to write new config to disk: %w", err)
				configFetcherResult <- result
				return
			}
			f.currentConfigDigest = configDigest
		}

		// Success case
		result.ConfigDigest = configDigest
		configFetcherResult <- result
		payload.CurrentActivity = "state synced successfully"
	}()

	// Collect results from all goroutines
	var allErrors []string
	receivedStateFetchers := 0
	receivedConfigFetcher := false

	// Wait for all results from the fetcher goroutines or cancellation
	for {
		select {
		case <-ctx.Done():
			log.Warn().Err(ctx.Err()).Msg("Operation cancelled")
			return ctx.Err()

		case stateResult := <-stateFetcherResults:
			receivedStateFetchers++

			if stateResult.Cancelled {
				log.Debug().Int("goroutine-id", stateResult.Index).Str("group", stateResult.URL).Msg("State fetcher cancelled")
			} else if stateResult.Error != nil {
				allErrors = append(allErrors, stateResult.Error.Error())
				log.Error().Err(stateResult.Error).Int("goroutine-id", stateResult.Index).Str("group", stateResult.URL).Msg("State fetcher failed")
				payload.CurrentActivity = "encountered error"
			} else {
				log.Info().Int("goroutine-id", stateResult.Index).Str("group", stateResult.URL).Msgf("State fetcher completed successfully for %s", stateResult.URL)
			}

		case configResult := <-configFetcherResult:
			receivedConfigFetcher = true

			if configResult.Cancelled {
				log.Debug().Msg("Config fetcher cancelled")
			} else if configResult.Error != nil {
				allErrors = append(allErrors, configResult.Error.Error())
				log.Error().Err(configResult.Error).Msg("Config fetcher failed")
				payload.CurrentActivity = "encountered error"
			} else {
				log.Info().Str("digest", configResult.ConfigDigest).Msg("Config fetcher completed successfully")
			}
		}

		// Check if we've received all results
		if receivedStateFetchers == len(f.stateMap) && receivedConfigFetcher {
			break
		}
	}

	// Return accumulated errors if any
	if len(allErrors) > 0 {
		payload.CurrentActivity = "encountered error"
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
