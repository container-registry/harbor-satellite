package replicate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/store"
	"container-registry.com/harbor-satellite/logger"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context, image string) error
	DeleteExtraImages(ctx context.Context, imgs []store.Image) error
}

type BasicReplicator struct {
	username     string
	password     string
	use_unsecure bool
	zot_url      string
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

func NewReplicator(context context.Context) Replicator {
	return &BasicReplicator{
		username:     config.GetHarborUsername(),
		password:     config.GetHarborPassword(),
		use_unsecure: config.UseUnsecure(),
		zot_url:      config.GetZotURL(),
	}
}

func (r *BasicReplicator) Replicate(ctx context.Context, image string) error {

	source := getPullSource(ctx, image)

	if source != "" {
		CopyImage(ctx, source)
	}
	return nil
}

func stripPrefix(imageName string) string {
	if idx := strings.Index(imageName, ":"); idx != -1 {
		return imageName[idx+1:]
	}
	return imageName
}

func (r *BasicReplicator) DeleteExtraImages(ctx context.Context, imgs []store.Image) error {
	log := logger.FromContext(ctx)
	zotUrl := os.Getenv("ZOT_URL")
	registry := os.Getenv("REGISTRY")
	repository := os.Getenv("REPOSITORY")
	image := os.Getenv("IMAGE")

	localRegistry := fmt.Sprintf("%s/%s/%s/%s", zotUrl, registry, repository, image)
	log.Info().Msgf("Local registry: %s", localRegistry)

	// Get the list of images from the local registry
	localImages, err := crane.ListTags(localRegistry)
	if err != nil {
		log.Error().Msgf("failed to list tags: %v", err)
		return err
	}

	// Create a map for quick lookup of the provided image list
	imageMap := make(map[string]struct{})
	for _, img := range imgs {
		// Strip the "album-server:" prefix from the image name
		strippedName := stripPrefix(img.Name)
		imageMap[strippedName] = struct{}{}
	}

	// Iterate over the local images and delete those not in the provided image list
	for _, localImage := range localImages {
		if _, exists := imageMap[localImage]; !exists {
			// Image is not in the provided list, delete it
			log.Info().Msgf("Deleting image: %s", localImage)
			err := crane.Delete(fmt.Sprintf("%s:%s", localRegistry, localImage))
			if err != nil {
				log.Error().Msgf("failed to delete image: %v", err)
				return err
			}
			log.Info().Msgf("Image deleted: %s", localImage)
		}
	}

	return nil
}

func getPullSource(ctx context.Context, image string) string {
	log := logger.FromContext(ctx)
	input := os.Getenv("USER_INPUT")
	scheme := os.Getenv("SCHEME")
	if strings.HasPrefix(scheme, "http://") || strings.HasPrefix(scheme, "https://") {
		url := os.Getenv("REGISTRY") + "/" + os.Getenv("REPOSITORY") + "/" + image
		return url
	} else {
		registryInfo, err := getFileInfo(ctx, input)
		if err != nil {
			log.Error().Msgf("Error getting file info: %v", err)
			return ""
		}
		registryURL := registryInfo.RegistryUrl
		registryURL = strings.TrimPrefix(registryURL, "https://")
		registryURL = strings.TrimSuffix(registryURL, "/v2/")

		// TODO: Handle multiple repositories
		repositoryName := registryInfo.Repositories[0].Repository

		return registryURL + "/" + repositoryName + "/" + image
	}
}

func getFileInfo(ctx context.Context, input string) (*RegistryInfo, error) {
	log := logger.FromContext(ctx)
	// Get the current working directory
	workingDir, err := os.Getwd()
	if err != nil {
		log.Error().Msgf("Error getting current directory: %v", err)
		return nil, err
	}

	// Construct the full path by joining the working directory and the input path
	fullPath := filepath.Join(workingDir, input)

	// Read the file
	jsonData, err := os.ReadFile(fullPath)
	if err != nil {
		log.Error().Msgf("Error reading file: %v", err)
		return nil, err
	}

	var registryInfo RegistryInfo
	err = json.Unmarshal(jsonData, &registryInfo)
	if err != nil {
		log.Error().Msgf("Error unmarshalling JSON data: %v", err)
		return nil, err
	}

	return &registryInfo, nil
}

func CopyImage(ctx context.Context, imageName string) error {
	log := logger.FromContext(ctx)
	log.Info().Msgf("Copying image: %s", imageName)
	zotUrl := os.Getenv("ZOT_URL")
	if zotUrl == "" {
		log.Error().Msg("ZOT_URL environment variable is not set")
		return fmt.Errorf("ZOT_URL environment variable is not set")
	}

	// Build the destination reference
	destRef := fmt.Sprintf("%s/%s", zotUrl, imageName)
	log.Info().Msgf("Destination reference: %s", destRef)

	// Get credentials from environment variables
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		log.Error().Msg("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
		return fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})
	options := []crane.Option{crane.WithAuth(auth)}
	if config.UseUnsecure() {
		options = append(options, crane.Insecure)
	}
	// Pull the image with authentication
	srcImage, err := crane.Pull(imageName, options...)
	if err != nil {
		log.Error().Msgf("Failed to pull image: %v", err)
		return fmt.Errorf("failed to pull image: %w", err)
	} else {
		log.Info().Msg("Image pulled successfully")
	}

	// Push the image to the destination registry
	push_options := []crane.Option{}
	if config.UseUnsecure() {
		push_options = append(push_options, crane.Insecure)
	}
	err = crane.Push(srcImage, destRef, push_options...)
	if err != nil {
		log.Error().Msgf("Failed to push image: %v", err)
		return fmt.Errorf("failed to push image: %w", err)
	} else {
		log.Info().Msg("Image pushed successfully")
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
