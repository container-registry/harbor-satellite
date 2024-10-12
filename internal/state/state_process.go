package state

import (
	"context"
	"encoding/json"
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
	useUnsecure       bool
	remoteRegistryURL string
	sourceRegistry    string
}

type FetchAndReplicateStateProcess struct {
	id                   uint64
	name                 string
	stateArtifactFetcher StateFetcher
	cronExpr             string
	isRunning            bool
	stateReader          StateReader
	notifier             notifier.Notifier
	mu                   *sync.Mutex
	authConfig           FetchAndReplicateAuthConfig
}

func NewFetchAndReplicateStateProcess(id uint64, cronExpr string, stateFetcher StateFetcher, notifier notifier.Notifier, username, password, remoteRegistryURL, sourceRegistryURL string, useUnsecure bool) *FetchAndReplicateStateProcess {
	return &FetchAndReplicateStateProcess{
		id:                   id,
		name:                 FetchAndReplicateStateProcessName,
		cronExpr:             cronExpr,
		isRunning:            false,
		stateArtifactFetcher: stateFetcher,
		notifier:             notifier,
		mu:                   &sync.Mutex{},
		authConfig: FetchAndReplicateAuthConfig{
			Username:          username,
			Password:          password,
			useUnsecure:       useUnsecure,
			remoteRegistryURL: remoteRegistryURL,
			sourceRegistry:    sourceRegistryURL,
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
	newStateFetched, err := f.FetchAndProcessState(log)
	if err != nil {
		return err
	}
	log.Info().Msg("State fetched successfully")
	deleteEntity, replicateEntity, newState := f.GetChanges(newStateFetched, log)
	f.LogChanges(deleteEntity, replicateEntity, log)
	if err := f.notifier.Notify(); err != nil {
		log.Error().Err(err).Msg("Error sending notification")
	}
	replicator := NewBasicReplicator(f.authConfig.Username, f.authConfig.Password, f.authConfig.remoteRegistryURL, f.authConfig.sourceRegistry, f.authConfig.useUnsecure)
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
	f.stateReader = newState
	return nil
}

func (f *FetchAndReplicateStateProcess) GetChanges(newState StateReader, log *zerolog.Logger) ([]ArtifactReader, []ArtifactReader, StateReader) {
	log.Info().Msg("Getting changes")

	var entityToDelete []ArtifactReader
	var entityToReplicate []ArtifactReader

	if f.stateReader == nil {
		log.Warn().Msg("Old state is nil")
		return entityToDelete, newState.GetArtifacts(), newState
	}

	// Remove artifacts with null tags from the new state
	newState = f.RemoveNullTagArtifacts(newState)

	// Create maps for quick lookups
	oldArtifactsMap := make(map[string]ArtifactReader)
	for _, oldArtifact := range f.stateReader.GetArtifacts() {
		tag := oldArtifact.GetTags()[0]
		oldArtifactsMap[oldArtifact.GetName()+"|"+tag] = oldArtifact
	}

	// Check new artifacts and update lists
	for _, newArtifact := range newState.GetArtifacts() {
		nameTagKey := newArtifact.GetName() + "|" + newArtifact.GetTags()[0]
		oldArtifact, exists := oldArtifactsMap[nameTagKey]

		if !exists {
			// New artifact doesn't exist in old state, add to replication list
			entityToReplicate = append(entityToReplicate, newArtifact)
		} else if newArtifact.GetDigest() != oldArtifact.GetDigest() {
			// Artifact exists but has changed, add to both lists
			entityToReplicate = append(entityToReplicate, newArtifact)
			entityToDelete = append(entityToDelete, oldArtifact)
		}

		// Remove processed old artifact from map
		delete(oldArtifactsMap, nameTagKey)
	}

	// Remaining artifacts in oldArtifactsMap should be deleted
	for _, oldArtifact := range oldArtifactsMap {
		entityToDelete = append(entityToDelete, oldArtifact)
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

func PrintPrettyJson(info interface{}, log *zerolog.Logger, message string) error {
	log.Warn().Msg("Printing pretty JSON")
	stateJSON, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("Error marshalling state to JSON")
		return err
	}
	log.Info().Msgf("%s: %s", message, stateJSON)
	return nil
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

func (f *FetchAndReplicateStateProcess) FetchAndProcessState(log *zerolog.Logger) (StateReader, error) {
	state := NewState()
	err := f.stateArtifactFetcher.FetchStateArtifact(&state)
	if err != nil {
		log.Error().Err(err).Msg("Error fetching state artifact")
		return nil, err
	}
	ProcessState(&state)
	return state, nil
}

func (f *FetchAndReplicateStateProcess) LogChanges(deleteEntity, replicateEntity []ArtifactReader, log *zerolog.Logger) {
	log.Warn().Msgf("Total artifacts to delete: %d", len(deleteEntity))
	log.Warn().Msgf("Total artifacts to replicate: %d", len(replicateEntity))
}
