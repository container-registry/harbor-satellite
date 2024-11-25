package state

import (
	"context"
	"fmt"

	"container-registry.com/harbor-satellite/logger"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context, replicationEntities []Entity) error
	// DeleteReplicationEntity deletes the image from the local registry.
	DeleteReplicationEntity(ctx context.Context, replicationEntity []Entity) error
}

type BasicReplicator struct {
	username          string
	password          string
	useUnsecure       bool
	remoteRegistryURL string
	sourceRegistry    string
}

func NewBasicReplicator(username, password, zotURL, sourceRegistry string, useUnsecure bool) Replicator {
	return &BasicReplicator{
		username:          username,
		password:          password,
		useUnsecure:       useUnsecure,
		remoteRegistryURL: zotURL,
		sourceRegistry:    sourceRegistry,
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
	auth := authn.FromConfig(authn.AuthConfig{
		Username: r.username,
		Password: r.password,
	})

	options := []crane.Option{crane.WithAuth(auth)}
	if r.useUnsecure {
		options = append(options, crane.Insecure)
	}

	for _, replicationEntity := range replicationEntities {

		log.Info().Msgf("Pulling image %s from repository %s at registry %s with tag %s", replicationEntity.GetName(), replicationEntity.GetRepository(), r.sourceRegistry, replicationEntity.GetTag())

		// Pull the image from the source registry
		srcImage, err := crane.Pull(fmt.Sprintf("%s/%s/%s:%s", r.sourceRegistry, replicationEntity.GetRepository(), replicationEntity.GetName(), replicationEntity.GetTag()), options...)
		if err != nil {
			log.Error().Msgf("Failed to pull image: %v", err)
			return err
		}

		// Convert Docker manifest to OCI manifest
		ociImage := mutate.MediaType(srcImage, types.OCIManifestSchema1)

		// Push the converted OCI image to the Zot registry
		err = crane.Push(ociImage, fmt.Sprintf("%s/%s/%s:%s", r.remoteRegistryURL, replicationEntity.GetRepository(), replicationEntity.GetName(), replicationEntity.GetTag()), options...)
		if err != nil {
			log.Error().Msgf("Failed to push image: %v", err)
			return err
		}
		log.Info().Msgf("Image %s pushed successfully", replicationEntity.GetName())

	}
	return nil
}

func (r *BasicReplicator) DeleteReplicationEntity(ctx context.Context, replicationEntity []Entity) error {
	log := logger.FromContext(ctx)
	auth := authn.FromConfig(authn.AuthConfig{
		Username: r.username,
		Password: r.password,
	})

	options := []crane.Option{crane.WithAuth(auth)}
	if r.useUnsecure {
		options = append(options, crane.Insecure)
	}

	for _, entity := range replicationEntity {
		log.Info().Msgf("Deleting image %s from repository %s at registry %s with tag %s", entity.GetName(), entity.GetRepository(), r.remoteRegistryURL, entity.GetTag())

		err := crane.Delete(fmt.Sprintf("%s/%s/%s:%s", r.remoteRegistryURL, entity.GetRepository(), entity.GetName(), entity.GetTag()), options...)
		if err != nil {
			log.Error().Msgf("Failed to delete image: %v", err)
			return err
		}
		log.Info().Msgf("Image %s deleted successfully", entity.GetName())
	}

	return nil
}
