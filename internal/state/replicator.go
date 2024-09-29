package state

import (
	"context"
	"fmt"
	"os"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context) error
}

type BasicReplicator struct {
	username     string
	password     string
	use_unsecure bool
	zot_url      string
	state_reader StateReader
}

type ImageInfo struct {
	Name string `json:"name"`
}

type Repository struct {
	Repository string      `json:"repository"`
	Images     []ImageInfo `json:"images"`
}

type RegistryInfo struct {
	RegistryUrl  string       `json:"registryUrl"`
	Repositories []Repository `json:"repositories"`
}

func BasicNewReplicator(state_reader StateReader) Replicator {
	return &BasicReplicator{
		username:     config.GetHarborUsername(),
		password:     config.GetHarborPassword(),
		use_unsecure: config.UseUnsecure(),
		zot_url:      config.GetZotURL(),
		state_reader: state_reader,
	}
}

func (r *BasicReplicator) Replicate(ctx context.Context) error {
	log := logger.FromContext(ctx)
	auth := authn.FromConfig(authn.AuthConfig{
		Username: r.username,
		Password: r.password,
	})

	options := []crane.Option{crane.WithAuth(auth)}
	if r.use_unsecure {
		options = append(options, crane.Insecure)
	}
	source_registry := r.state_reader.GetRegistryURL()
	for _, artifact := range r.state_reader.GetArtifacts() {
		// Extract the image name from the repository of the artifact
		repo, image, err := utils.GetRepositoryAndImageNameFromArtifact(artifact.GetRepository())
		if err != nil {
			log.Error().Msgf("Error getting repository and image name: %v", err)
			return err
		}
		log.Info().Msgf("Pulling image %s from repository %s at registry %s", image, repo, source_registry)
		// Pull the image at the given repository at the source registry
		_, err = crane.Pull(fmt.Sprintf("%s/%s/%s", source_registry, repo, image), options...)
		if err != nil {
			logger.FromContext(ctx).Error().Msgf("Failed to pull image: %v", err)
			return err
		}
		// Push the image to the local registry
		// err = crane.Push(srcImage, fmt.Sprintf("%s/%s", r.zot_url, image), options...)
		// if err != nil {
		// 	logger.FromContext(ctx).Error().Msgf("Failed to push image: %v", err)
		// 	return err
		// }
		log.Info().Msgf("Image %s pushed successfully", image)
	}
	// Delete ./local-oci-layout directory
	// This is required because it is a temporary directory used by crane to pull and push images to and from
	// And crane does not automatically clean it
	if err := os.RemoveAll("./local-oci-layout"); err != nil {
		log.Error().Msgf("Failed to remove directory: %v", err)
		return fmt.Errorf("failed to remove directory: %w", err)
	}
	return nil
}
