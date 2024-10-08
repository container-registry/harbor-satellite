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
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context) error
}

type BasicReplicator struct {
	username    string
	password    string
	useUnsecure bool
	zotURL      string
	stateReader StateReader
}

func NewBasicReplicator(state_reader StateReader) Replicator {
	return &BasicReplicator{
		username:    config.GetHarborUsername(),
		password:    config.GetHarborPassword(),
		useUnsecure: config.UseUnsecure(),
		zotURL:      config.GetZotURL(),
		stateReader: state_reader,
	}
}
// Replicate replicates images from the source registry to the Zot registry.
func (r *BasicReplicator) Replicate(ctx context.Context) error {
	log := logger.FromContext(ctx)
	auth := authn.FromConfig(authn.AuthConfig{
		Username: r.username,
		Password: r.password,
	})

	options := []crane.Option{crane.WithAuth(auth)}
	if r.useUnsecure {
		options = append(options, crane.Insecure)
	}
	sourceRegistry := utils.FormatRegistryUrl(config.GetRemoteRegistryURL())

	for _, artifact := range r.stateReader.GetArtifacts() {
		// Extract the image name and repository from the artifact
		repo, image, err := utils.GetRepositoryAndImageNameFromArtifact(artifact.GetRepository())
		if err != nil {
			log.Error().Msgf("Error getting repository and image name: %v", err)
			return err
		}
		allTags := artifact.GetTags()

		// Pull and replicate all tags of the image
		for _, tag := range allTags {
			log.Info().Msgf("Pulling image %s from repository %s at registry %s with tag %s", image, repo, sourceRegistry, tag)

			// Pull the image from the source registry
			srcImage, err := crane.Pull(fmt.Sprintf("%s/%s/%s:%s", sourceRegistry, image, image, tag), options...)
			if err != nil {
				log.Error().Msgf("Failed to pull image: %v", err)
				return err
			}

			// Convert Docker manifest to OCI manifest
			ociImage := mutate.MediaType(srcImage, types.OCIManifestSchema1)
			
			// Push the converted OCI image to the Zot registry
			err = crane.Push(ociImage, fmt.Sprintf("%s/%s", r.zotURL, image), options...)
			if err != nil {
				log.Error().Msgf("Failed to push image: %v", err)
				return err
			}
			log.Info().Msgf("Image %s pushed successfully", image)
		}
	}

	// Clean up the temporary directory
	if err := os.RemoveAll("./local-oci-layout"); err != nil {
		log.Error().Msgf("Failed to remove directory: %v", err)
		return fmt.Errorf("failed to remove directory: %w", err)
	}
	return nil
}
