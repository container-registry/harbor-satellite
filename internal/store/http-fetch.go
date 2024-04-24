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
		{Reference: "alpine:3.1"},
		{Reference: "alpine:3.2"},
		{Reference: "alpine:3.3"},
	}
	return images, nil
}
