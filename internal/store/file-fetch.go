package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type FileImageList struct {
	Path string
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

	fmt.Println("Reading from:", client.Path)

	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		return nil, err
	}

	// Define a struct to match the JSON structure
	var imageData struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}

	// Parse the JSON data
	err = json.Unmarshal(data, &imageData)
	if err != nil {
		return nil, err
	}

	// Convert the parsed data into a slice of Image structs
	images := make([]Image, len(imageData.Results))
	for i, result := range imageData.Results {
		images[i] = Image{
			Reference: result.Name,
		}
	}
	fmt.Println("Fetched", len(images), "images :", images)

	return images, nil
}

func (client *FileImageList) GetHash(ctx context.Context) (string, error) {
	// Read the file
	data, err := os.ReadFile(client.Path)
	if err != nil {
		return "", err
	}

	// Hash and return the body
	hash := sha256.Sum256(data)
	hashString := hex.EncodeToString(hash[:])

	return hashString, nil
}
