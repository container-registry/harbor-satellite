package store

import (
	"context"
)

type RemoteImageList struct {
	BaseURL    string
	Repository string
}

func RemoteImageListFetcher() *RemoteImageList {
	return &RemoteImageList{
		BaseURL:    "",
		Repository: "",
	}
}

func (client *RemoteImageList) List(ctx context.Context) ([]Image, error) {
	// Placeholder for fetching images from a remote registry
	images := []Image{
		{"alpine:3.11"},
		{"alpine:3.12"},
		{"alpine:3.13"},
	}
	return images, nil
}

func (client *RemoteImageList) GetHash(ctx context.Context) (string, error) {
	// Placeholder for fetching image hash from a remote registry
	return "hash123", nil
}
