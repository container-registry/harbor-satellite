package state

import (
	"context"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/transfer"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context, replicationEntities []Entity) error
	// DeleteReplicationEntity deletes the image from the local registry.
	DeleteReplicationEntity(ctx context.Context, replicationEntity []Entity) error
}

type BasicReplicator struct {
	useUnsecure       bool
	sourceUsername    string
	sourcePassword    string
	sourceRegistry    string
	remoteRegistryURL string
	remoteUsername    string
	remotePassword    string
	transferMeter     *transfer.TransferMeter
}

func NewBasicReplicator(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword string, useUnsecure bool, transferMeter *transfer.TransferMeter) Replicator {
	return &BasicReplicator{
		sourceUsername:    sourceUsername,
		sourcePassword:    sourcePassword,
		useUnsecure:       useUnsecure,
		remoteRegistryURL: remoteURL,
		sourceRegistry:    sourceRegistry,
		remoteUsername:    remoteUsername,
		remotePassword:    remotePassword,
		transferMeter:     transferMeter,
	}
}

// Entity represents an image or artifact which needs to be handled by the replicator
type Entity struct {
	Name       string
	Repository string
	Tag        string
	Digest     string
}

func (e Entity) GetName() string {
	return e.Name
}

func (e Entity) GetRepository() string {
	return e.Repository
}

func (e Entity) GetTag() string {
	return e.Tag
}

// Replicate replicates images from the source registry to the Zot registry.
func (r *BasicReplicator) Replicate(ctx context.Context, replicationEntities []Entity) error {
	log := logger.FromContext(ctx)
	for _, entity := range replicationEntities {
		sourceImage := fmt.Sprintf("%s/%s:%s", r.sourceRegistry, entity.Repository, entity.Tag)
		destinationImage := fmt.Sprintf("%s/%s:%s", r.remoteRegistryURL, entity.Repository, entity.Tag)

		// Get image size before transfer
		img, err := crane.Pull(sourceImage, crane.WithAuth(authn.FromConfig(authn.AuthConfig{
			Username: r.sourceUsername,
			Password: r.sourcePassword,
		})))
		if err != nil {
			return fmt.Errorf("failed to pull image %s: %v", sourceImage, err)
		}

		// Calculate total size of all layers
		layers, err := img.Layers()
		if err != nil {
			return fmt.Errorf("failed to get layers for image %s: %v", sourceImage, err)
		}

		var totalSize int64
		for _, layer := range layers {
			size, err := layer.Size()
			if err != nil {
				return fmt.Errorf("failed to get layer size: %v", err)
			}
			totalSize += size
		}

		// Check transfer limits before proceeding
		if r.transferMeter != nil {
			if err := r.transferMeter.RecordTransfer(totalSize); err != nil {
				return fmt.Errorf("transfer limit exceeded: %v", err)
			}
		}

		// Push the image to destination
		err = crane.Push(img, destinationImage, crane.WithAuth(authn.FromConfig(authn.AuthConfig{
			Username: r.remoteUsername,
			Password: r.remotePassword,
		})))
		if err != nil {
			return fmt.Errorf("failed to push image %s: %v", destinationImage, err)
		}

		log.Info().
			Str("source", sourceImage).
			Str("destination", destinationImage).
			Int64("size", totalSize).
			Msg("Successfully replicated image")
	}

	return nil
}

// DeleteReplicationEntity deletes the image from the local registry.
func (r *BasicReplicator) DeleteReplicationEntity(ctx context.Context, replicationEntities []Entity) error {
	log := logger.FromContext(ctx)
	for _, entity := range replicationEntities {
		destinationImage := fmt.Sprintf("%s/%s:%s", r.remoteRegistryURL, entity.Repository, entity.Tag)
		err := crane.Delete(destinationImage, crane.WithAuth(authn.FromConfig(authn.AuthConfig{
			Username: r.remoteUsername,
			Password: r.remotePassword,
		})))
		if err != nil {
			return fmt.Errorf("failed to delete image %s: %v", destinationImage, err)
		}

		log.Info().
			Str("image", destinationImage).
			Msg("Successfully deleted image")
	}

	return nil
}
