package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"container-registry.com/harbor-satellite/internal/notifier"
	"container-registry.com/harbor-satellite/logger"
	"github.com/rs/zerolog"
)

const FetchAndReplicateStateProcessName string = "fetch-replicate-state-process"

const DefaultFetchAndReplicateStateTimePeriod string = "00h00m05s"

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
	PrintPrettyJson(newStateFetched, log)
	log.Info().Msg("State fetched successfully")
	log.Info().Msg("Checking if state has changed")

	deleteEntity, replicateEntity, newState := f.GetChanges(newStateFetched, log)
	log.Info().Msgf("Total artifacts to delete: %d", len(deleteEntity))
	for _, entity := range deleteEntity {
		log.Info().Msgf("Artifact: %s, Tag: %s", entity.GetName(), entity.GetTags()[0])
	}
	log.Info().Msgf("Total artifacts to replicate: %d", len(replicateEntity))
	log.Info().Msg("Artifacts to replicate:")
	for _, entity := range replicateEntity {
		log.Info().Msgf("Artifact: %s, Tag: %s", entity.GetName(), entity.GetTags()[0])
	}
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
	var entityToDelete []ArtifactReader
	var entityToReplicate []ArtifactReader
	if f.stateReader == nil {
		return entityToDelete, newState.GetArtifacts(), newState
	}
	log.Warn().Msg("Old state reader is not nil")
	PrintPrettyJson(f.stateReader, log)
	// Remove all the artifacts from the new state reader whose tags are null to make sure if a tags image is updated then it is replicated
	newState = f.stateReader.RemoveArtifactsWithNullTags(newState)

	newArtifacts := newState.GetArtifacts()

	for _, artifact := range newArtifacts {
		log.Info().Msgf("Checking artifact: %s", artifact.GetName())
		// Check if this artifact is present in the old state reader or not
		oldArtifact := f.stateReader.GetArtifactByNameAndTag(artifact.GetName(), artifact.GetTags()[0])
		if oldArtifact == nil {
			log.Info().Msgf("Artifact: %s not present in the old state reader", artifact.GetName())
			// This artifact is not present in the old state reader, so we need to replicate it
			entityToReplicate = append(entityToReplicate, artifact)
			continue
		}
		// This artifact is present in the old state reader, so we need to check if it has changed or not by comparing the digest
		if artifact.GetDigest() != oldArtifact.GetDigest() {
			// This artifact has changed, so we need to replicate it
			entityToReplicate = append(entityToReplicate, artifact)
			// We also need to delete from the remote registry
			entityToDelete = append(entityToDelete, oldArtifact)
		}
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

func (f *FetchAndReplicateStateProcess) RemoveNullTagArtifacts(state StateReader) StateReader {
	newStateReader := state.RemoveArtifactsWithNullTags(state)
	return newStateReader
}

func PrintPrettyJson(info interface{}, log *zerolog.Logger) error {
	log.Warn().Msg("Printing pretty JSON")
	stateJSON, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("Error marshalling state to JSON")
		return err
	}
	log.Info().Msgf("Fetched state: %s", stateJSON)
	os.Exit(0)
	return nil
}
