package state

import (
	"context"
	"fmt"
	"sync"

	"container-registry.com/harbor-satellite/internal/notifier"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"github.com/rs/zerolog"
)

const FetchAndReplicateStateProcessName string = "fetch-replicate-state-process"

const DefaultFetchAndReplicateStateTimePeriod string = "00h00m010s"

type FetchAndReplicateAuthConfig struct {
	Username          string
	Password          string
	UseUnsecure       bool
	RemoteRegistryURL string
	SourceRegistry    string
}

type FetchAndReplicateStateProcess struct {
	id         uint64
	name       string
	cronExpr   string
	isRunning  bool
	stateMap   []StateMap
	notifier   notifier.Notifier
	mu         *sync.Mutex
	authConfig FetchAndReplicateAuthConfig
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

func NewFetchAndReplicateStateProcess(id uint64, cronExpr string, notifier notifier.Notifier, username, password, remoteRegistryURL, sourceRegistryURL string, useUnsecure bool, states []string) *FetchAndReplicateStateProcess {
	return &FetchAndReplicateStateProcess{
		id:        id,
		name:      FetchAndReplicateStateProcessName,
		cronExpr:  cronExpr,
		isRunning: false,
		notifier:  notifier,
		mu:        &sync.Mutex{},
		stateMap:  NewStateMap(states),
		authConfig: FetchAndReplicateAuthConfig{
			Username:          username,
			Password:          password,
			UseUnsecure:       useUnsecure,
			RemoteRegistryURL: remoteRegistryURL,
			SourceRegistry:    sourceRegistryURL,
		},
	}
}

func (f *FetchAndReplicateStateProcess) Execute(ctx context.Context) error {
	log := logger.FromContext(ctx)
	if !f.start() {
		log.Warn().Msg("Process already running")
		return fmt.Errorf("process %s already running", f.GetName())
	}
	defer f.stop()

	for i := range f.stateMap {
		log.Info().Msgf("Processing state for %s", f.stateMap[i].url)
		stateFetcher, err := processInput(f.stateMap[i].url, f.authConfig.Username, f.authConfig.Password, log)
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

		replicator := NewBasicReplicator(f.authConfig.Username, f.authConfig.Password, f.authConfig.RemoteRegistryURL, f.authConfig.SourceRegistry, f.authConfig.UseUnsecure)
		// Delete the entities from the remote registry
		if err := replicator.DeleteReplicationEntity(ctx, deleteEntity); err != nil {
			log.Error().Err(err).Msg("Error deleting entities")
			return err
		}
		// Replicate the entities to the remote registry
		if err := replicator.Replicate(ctx, replicateEntity); err != nil {
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
