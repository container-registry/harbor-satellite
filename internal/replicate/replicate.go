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
	// Replicate copies images from the source registry to the local registry.
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

func (r *BasicReplicator) Replicate(ctx context.Context, image string) error {
	source := getPullSource(image)

	if source != "" {
		CopyImage(source)
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
	zotUrl := os.Getenv("ZOT_URL")
	host := os.Getenv("HOST")
	registry := os.Getenv("REGISTRY")
	repository := os.Getenv("REPOSITORY")

	localRegistry := fmt.Sprintf("%s/%s/%s/%s", zotUrl, host, registry, repository)
	fmt.Println("Syncing local registry:", localRegistry)

	// Get the list of images from the local registry
	localImages, err := crane.ListTags(localRegistry)
	if err != nil {
		return fmt.Errorf("failed to get local registry catalog: %w", err)
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
			fmt.Print("Deleting image: ", localRegistry+":"+localImage, " ... ")
			err := crane.Delete(fmt.Sprintf("%s:%s", localRegistry, localImage))
			if err != nil {
				fmt.Printf("failed to delete image %s: %v\n", localImage, err)
				return nil
			}
			fmt.Printf("Deleted image: %s\n", localImage)
		}
	}

	return nil
}

func getPullSource(image string) string {
	input := os.Getenv("USER_INPUT")
	scheme := os.Getenv("SCHEME")
	if strings.HasPrefix(scheme, "http://") || strings.HasPrefix(scheme, "https://") {
		url := os.Getenv("HOST") + "/" + os.Getenv("REGISTRY") + "/" + image
		return url
	} else {
		registryInfo, err := getFileInfo(input)
		if err != nil {
			return "Error loading file info: " + err.Error()
		}
		registryURL := registryInfo.RegistryUrl
		registryURL = strings.TrimPrefix(registryURL, "https://")
		registryURL = strings.TrimSuffix(registryURL, "v2/")

		// TODO: Handle multiple repositories
		repositoryName := registryInfo.Repositories[0].Repository

		return registryURL + repositoryName + "/" + image
	}
}

func getFileInfo(input string) (*RegistryInfo, error) {
	// Get the current working directory
	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Construct the full path by joining the working directory and the input path
	fullPath := filepath.Join(workingDir, input)

	// Read the file
	jsonData, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var registryInfo RegistryInfo
	err = json.Unmarshal(jsonData, &registryInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return &registryInfo, nil
}

func CopyImage(imageName string) error {
	fmt.Println("Copying image:", imageName)
	zotUrl := os.Getenv("ZOT_URL")
	if zotUrl == "" {
		return fmt.Errorf("ZOT_URL environment variable is not set")
	}

	// Clean up the image name by removing any host part
	cleanedImageName := removeHostName(imageName)
	destRef := fmt.Sprintf("%s/%s", zotUrl, cleanedImageName)
	fmt.Println("Destination reference:", destRef)

	// Get credentials from environment variables
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		return fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})

	// Pull the image with authentication
	srcImage, err := crane.Pull(imageName, crane.WithAuth(auth), crane.Insecure)
	if err != nil {
		fmt.Printf("Failed to pull image: %v\n", err)
		return fmt.Errorf("failed to pull image: %w", err)
	} else {
		fmt.Println("Image pulled successfully")
		fmt.Printf("Pulled image details: %+v\n", srcImage)
	}

	// Push the image to the destination registry
	err = crane.Push(srcImage, destRef, crane.Insecure)
	if err != nil {
		fmt.Printf("Failed to push image: %v\n", err)
		return fmt.Errorf("failed to push image: %w", err)
	} else {
		fmt.Println("Image pushed successfully")
		fmt.Printf("Pushed image to: %s\n", destRef)
	}

	// Delete ./local-oci-layout directory
	// This is required because it is a temporary directory used by crane to pull and push images to and from
	// And crane does not automatically clean it
	if err := os.RemoveAll("./local-oci-layout"); err != nil {
		fmt.Printf("Failed to remove directory: %v\n", err)
		return fmt.Errorf("failed to remove directory: %w", err)
	}

	// // Use crane.Copy to copy the image directly without pulling & storing
	// // this only works when remote & local registries are same.
	// err := crane.Copy(imageName, destRef, crane.WithAuth(auth), crane.Insecure)
	// if err != nil {
	// 	fmt.Printf("Failed to copy image: %v\n", err)
	// 	return fmt.Errorf("failed to copy image: %w", err)
	// } else {
	// 	fmt.Println("Image copied successfully")
	// 	fmt.Printf("Copied image from %s to: %s\n", imageName, destRef)
	// }

	return nil
}

// Split the imageName by "/" and take only the parts after the hostname
func removeHostName(imageName string) string {
	parts := strings.Split(imageName, "/")
	if len(parts) > 1 {
		return strings.Join(parts[1:], "/")
	}

	return imageName
}
