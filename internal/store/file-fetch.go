package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"container-registry.com/harbor-satellite/logger"
)

type FileImageList struct {
	Path string
}

type Repository struct {
	Repository string `json:"repository"`
	Images     []struct {
		Name string `json:"name"`
	} `json:"images"`
}

type ImageData struct {
	RegistryUrl  string       `json:"registryUrl"`
	Repositories []Repository `json:"repositories"`
}

func (f *FileImageList) Type(ctx context.Context) string {
	return "File"
}

func FileImageListFetcher(ctx context.Context, relativePath string) *FileImageList {
	log := logger.FromContext(ctx)
	// Get the current working directory
	dir, err := os.Getwd()
	if err != nil {
		log.Error().Err(err).Msg("Error getting current directory")
		return nil
	}

	// Construct the absolute path from the relative path
	absPath := filepath.Join(dir, relativePath)

	return &FileImageList{
		Path: absPath,
	}
}

func (client *FileImageList) List(ctx context.Context) ([]Image, error) {
	log := logger.FromContext(ctx)
	var images []Image

	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		log.Error().Err(err).Msg("Error reading file")
		return nil, err
	}

	var imageData ImageData
	// Parse the JSON data
	err = json.Unmarshal(data, &imageData)
	if err != nil {
		log.Error().Err(err).Msg("Error unmarshalling JSON data")
		return nil, err
	}

	// Iterate over the repositories
	for _, repo := range imageData.Repositories {
		// Iterate over the images in each repository
		for _, image := range repo.Images {
			// Add each "name" value to the images slice
			images = append(images, Image{Name: image.Name})
		}
	}

	return images, nil
}

func (client *FileImageList) GetDigest(ctx context.Context, tag string) (string, error) {
	return "Not implemented yet", nil
}
