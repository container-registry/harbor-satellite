package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileImageList struct {
	Path string
}

type ImageData struct {
	Images []struct {
		Name          string `json:"name"`
		Digest        string `json:"digest"`
		RepositoryUrl string `json:"repositoryUrl"`
	} `json:"images"`
}

func (f *FileImageList) Type() string {
	return "File"
}

func FileImageListFetcher(relativePath string) *FileImageList {
	// Get the current working directory
	dir, err := os.Getwd()
	if err != nil {
		fmt.Println("Error getting current directory:", err)
		return nil
	}

	// Construct the absolute path from the relative path
	absPath := filepath.Join(dir, relativePath)

	return &FileImageList{
		Path: absPath,
	}
}

func (client *FileImageList) List(ctx context.Context) ([]Image, error) {
	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		return nil, err
	}

	var imageData ImageData
	// Parse the JSON data
	err = json.Unmarshal(data, &imageData)
	if err != nil {
		return nil, err
	}

	// Convert the parsed data into a slice of Image structs
	images := make([]Image, len(imageData.Images))
	for i, image := range imageData.Images {
		images[i] = Image{
			Reference: image.Name,
			Digest:    image.Digest,
		}
	}
	fmt.Println("Fetched", len(images), "images :", images)
	// Print the pull commands to test if stored data is correct and sufficient
	fmt.Println("Pull commands for tests :")
	client.GetPullCommands(ctx)

	return images, nil
}

func (client *FileImageList) GetDigest(ctx context.Context, tag string) (string, error) {
	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		return "", err
	}

	var imageData ImageData
	// Parse the JSON data
	err = json.Unmarshal(data, &imageData)
	if err != nil {
		return "", err
	}

	if tag == "" {
		return "", fmt.Errorf("tag cannot be empty")
	}

	// Iterate over the images to find the one with the matching tag
	for _, image := range imageData.Images {
		if strings.Contains(image.Name, tag) {
			return image.Digest, nil
		}
	}
	// If no image with the matching tag is found, return an error
	return "", fmt.Errorf("image with tag %s not found", tag)
}

func (client *FileImageList) GetTag(ctx context.Context, digest string) (string, error) {
	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		return "", err
	}

	var imageData ImageData
	// Parse the JSON data
	err = json.Unmarshal(data, &imageData)
	if err != nil {
		return "", err
	}

	if digest == "" {
		return "", fmt.Errorf("digest cannot be empty")
	}

	// Iterate over the images to find the one with the matching digest
	for _, image := range imageData.Images {
		if image.Digest == digest {
			return image.Name, nil
		}
	}
	// If no image with the matching digest is found, return an error
	return "", fmt.Errorf("image with digest %s not found", digest)

}

func (client *FileImageList) GetPullCommands(ctx context.Context) {
	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	var imageData ImageData
	// Parse the JSON data
	err = json.Unmarshal(data, &imageData)
	if err != nil {
		fmt.Println("Error parsing JSON:", err)
		return
	}

	// Iterate over the images to construct and print the pull command for each
	for _, image := range imageData.Images {
		harborUrl := strings.TrimPrefix(image.RepositoryUrl, "https://")
		harborUrl = strings.Replace(harborUrl, ".io/v2", ".io", -1)

		// Extract the first part of the image name
		parts := strings.Split(image.Name, ":")
		if len(parts) > 0 {
			// Append the first part of the split result to harborUrl
			harborUrl += "/" + parts[0]
		}

		pullCommand := fmt.Sprintf("docker pull %s@%s", harborUrl, image.Digest)
		fmt.Println(pullCommand)
	}
}

func (client *FileImageList) ListDigests(ctx context.Context) ([]string, error) {
	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		return nil, err
	}

	var imageData ImageData
	// Parse the JSON data
	err = json.Unmarshal(data, &imageData)
	if err != nil {
		return nil, err
	}

	// Prepare a slice to store the digests
	digests := make([]string, len(imageData.Images))
	for i, image := range imageData.Images {
		digests[i] = image.Digest
	}
	return digests, nil
}
