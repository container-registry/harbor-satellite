package replicate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context, image string) error
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

	// TODO: Implement deletion of images from the local registry that are not present in the source registry
	// Probably use crane.Catalog to get a list of images in the local registry and compare to incoming image list
	// Then use crane.Delete to delete those images

	source := getPullSource(image)

	if source != "" {
		CopyImage(source)
	}
	return nil
}

func getPullSource(image string) string {
	input := os.Getenv("USER_INPUT")
	if os.Getenv("SCHEME") == "https://" {
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

	srcRef := imageName
	destRef := zotUrl + imageName

	// Pull the image and specify a destination directory
	srcImage, err := crane.Pull(srcRef)
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	} else {
		fmt.Println("Image pulled successfully")
	}

	// Push the image to the destination registry
	err = crane.Push(srcImage, destRef)
	if err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	} else {
		fmt.Println("Image pushed successfully")
	}

	// Delete ./local-oci-layout directory if it already exists
	// This is required because it is a temporary directory used by crane to pull and push images to and from
	if err := os.RemoveAll("./local-oci-layout"); err != nil {
		return fmt.Errorf("failed to remove directory: %w", err)
	}

	return nil
}
