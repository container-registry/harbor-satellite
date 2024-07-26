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

type Replicator interface {
	Replicate(ctx context.Context, image string) error
	DeleteExtraImages(ctx context.Context, imgs []store.Image) error
}

type BasicReplicator struct{}

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

func NewReplicator() Replicator {
	return &BasicReplicator{}
}

// Replicate copies an image from  source registry to  local registry.
func (r *BasicReplicator) Replicate(ctx context.Context, image string) error {
	source := getPullSource(image)
	if source == "" {
		return fmt.Errorf("source not found for image: %s", image)
	}
	return CopyImage(source)
}

// stripPrefix removes  prefix from  image name.
func stripPrefix(imageName string) string {
	if idx := strings.Index(imageName, ":"); idx != -1 {
		return imageName[idx+1:]
	}
	return imageName
}

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
				return fmt.Errorf("failed to delete image %s: %v\n", localImage, err)
			}
			fmt.Printf("Deleted image: %s\n", localImage)
		}
	}

	return nil
}

// getPullSource constructs  source URL for pulling an image.
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

	registryURL := strings.TrimSuffix(
		strings.TrimPrefix(registryInfo.RegistryUrl, "https://"),
		"v2/",
	)
	repositoryName := registryInfo.Repositories[0].Repository

	return fmt.Sprintf("%s/%s/%s", registryURL, repositoryName, image)
}

// getFileInfo reads and unmarshals  registry info from a JSON file.
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

// CopyImage pulls an image from  source and pushes it to  destination.
func CopyImage(imageName string) error {
	fmt.Println("Copying image:", imageName)
	destRef := fmt.Sprintf("%s/%s", os.Getenv("ZOT_URL"), removeHostName(imageName))

	auth := authn.FromConfig(authn.AuthConfig{
		Username: os.Getenv("HARBOR_USERNAME"),
		Password: os.Getenv("HARBOR_PASSWORD"),
	})

	err := crane.Copy(imageName, destRef, crane.Insecure, crane.WithAuth(auth))
	if err != nil {
		return fmt.Errorf("Error in copying image from %s to %s: %v", imageName, destRef, err)
	}

	fmt.Println("Image pushed successfully")
	fmt.Printf("Pushed image to: %s\n", destRef)

	if err := os.RemoveAll("./local-oci-layout"); err != nil {
		return fmt.Errorf("failed to remove temporary directory: %w", err)
	}

	return nil
}

// removeHostName removes  hostname from  image name.
func removeHostName(imageName string) string {
	parts := strings.SplitN(imageName, "/", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return imageName
}

// getEnvRegistryPath constructs  local registry URL from environment variables.
func getEnvRegistryPath() string {
	return fmt.Sprintf("%s/%s/%s",
		os.Getenv("ZOT_URL"),
		os.Getenv("REGISTRY"),
		os.Getenv("REPOSITORY"))
}

// getWorkingDir returns  current working directory.
func getWorkingDir() string {
	workingDir, err := os.Getwd()
	if err != nil {
		panic(fmt.Errorf("failed to get working directory: %w", err))
	}
	return workingDir
}
