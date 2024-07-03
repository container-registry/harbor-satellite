package replicate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"container-registry.com/harbor-satellite/internal/store"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

// Replicator interface for image replication and deletion.
type Replicator interface {
	Replicate(ctx context.Context, image string) error
	DeleteExtraImages(ctx context.Context, imgs []store.Image) error
}

// BasicReplicator implements the Replicator interface.
type BasicReplicator struct{}

// ImageInfo holds the name of an image.
type ImageInfo struct {
	Name string `json:"name"`
}

// Repository holds the repository name and associated images.
type Repository struct {
	Repository string      `json:"repository"`
	Images     []ImageInfo `json:"images"`
}

// RegistryInfo holds the registry URL and repositories information.
type RegistryInfo struct {
	RegistryUrl  string       `json:"registryUrl"`
	Repositories []Repository `json:"repositories"`
}

// NewReplicator creates a new BasicReplicator.
func NewReplicator() Replicator {
	return &BasicReplicator{}
}

// Replicate copies an image from the source registry to the local registry.
func (r *BasicReplicator) Replicate(ctx context.Context, image string) error {
	source := getPullSource(image)
	if source == "" {
		return fmt.Errorf("source not found for image: %s", image)
	}
	return CopyImage(source)
}

// stripPrefix removes the prefix from the image name.
func stripPrefix(imageName string) string {
	if idx := strings.Index(imageName, ":"); idx != -1 {
		return imageName[idx+1:]
	}
	return imageName
}

// DeleteExtraImages removes images from the local registry not in the provided list.
func (r *BasicReplicator) DeleteExtraImages(ctx context.Context, imgs []store.Image) error {
	localRegistry := getEnvRegistryPath()

	fmt.Println("Syncing local registry:", localRegistry)

	localImages, err := crane.ListTags(localRegistry)
	if err != nil {
		return fmt.Errorf("failed to get local registry catalog: %w", err)
	}

	imageMap := make(map[string]struct{})
	for _, img := range imgs {
		imageMap[stripPrefix(img.Name)] = struct{}{}
	}

	for _, localImage := range localImages {
		if _, exists := imageMap[localImage]; !exists {
			if err := crane.Delete(fmt.Sprintf("%s:%s", localRegistry, localImage)); err != nil {
				fmt.Printf("failed to delete image %s: %v\n", localImage, err)
				return err
			}
			fmt.Printf("Deleted image: %s\n", localImage)
		}
	}

	return nil
}

// getPullSource constructs the source URL for pulling an image.
func getPullSource(image string) string {
	scheme := os.Getenv("SCHEME")
	if strings.HasPrefix(scheme, "http://") || strings.HasPrefix(scheme, "https://") {
		return fmt.Sprintf("%s/%s/%s", os.Getenv("HOST"), os.Getenv("REGISTRY"), image)
	}

	registryInfo, err := getFileInfo(os.Getenv("USER_INPUT"))
	if err != nil {
		fmt.Printf("Error loading file info: %v\n", err)
		return ""
	}

	registryURL := strings.TrimSuffix(strings.TrimPrefix(registryInfo.RegistryUrl, "https://"), "v2/")
	repositoryName := registryInfo.Repositories[0].Repository

	return fmt.Sprintf("%s%s/%s", registryURL, repositoryName, image)
}

// getFileInfo reads and unmarshals the registry info from a JSON file.
func getFileInfo(input string) (*RegistryInfo, error) {
	fullPath := filepath.Join(getWorkingDir(), input)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var registryInfo RegistryInfo
	if err := json.Unmarshal(data, &registryInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return &registryInfo, nil
}

// CopyImage pulls an image from the source and pushes it to the destination.
func CopyImage(imageName string) error {
	fmt.Println("Copying image:", imageName)
	destRef := fmt.Sprintf("%s/%s", os.Getenv("ZOT_URL"), removeHostName(imageName))

	auth := authn.FromConfig(authn.AuthConfig{
		Username: os.Getenv("HARBOR_USERNAME"),
		Password: os.Getenv("HARBOR_PASSWORD"),
	})

	srcImage, err := crane.Pull(imageName, crane.WithAuth(auth), crane.Insecure)
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	if err := crane.Push(srcImage, destRef, crane.Insecure); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}
	fmt.Println("Image pushed successfully")
	fmt.Printf("Pushed image to: %s\n", destRef)

	if err := os.RemoveAll("./local-oci-layout"); err != nil {
		return fmt.Errorf("failed to remove temporary directory: %w", err)
	}

	return nil
}

// removeHostName removes the hostname from the image name.
func removeHostName(imageName string) string {
	parts := strings.SplitN(imageName, "/", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return imageName
}

// getEnvRegistryPath constructs the local registry URL from environment variables.
func getEnvRegistryPath() string {
	return fmt.Sprintf("%s/%s/%s/%s",
		os.Getenv("ZOT_URL"),
		os.Getenv("HOST"),
		os.Getenv("REGISTRY"),
		os.Getenv("REPOSITORY"))
}

// getWorkingDir returns the current working directory.
func getWorkingDir() string {
	workingDir, err := os.Getwd()
	if err != nil {
		panic(fmt.Errorf("failed to get working directory: %w", err))
	}
	return workingDir
}
