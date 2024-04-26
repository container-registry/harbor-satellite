package store

import (
	"context"
)

type FileImageList struct {
	Path string
}

func FileImageListFetcher() *FileImageList {
	return &FileImageList{
		Path: "images.json",
	}
}

func (client *FileImageList) List(ctx context.Context) ([]Image, error) {
	// Placeholder for fetching images from a file
	images := []Image{
		{"alpine:3.1"},
		{"alpine:3.2"},
		{"alpine:3.3"},
	}
	return images, nil
}

func (client *FileImageList) GetHash(ctx context.Context) (string, error) {
	// Placeholder for fetching image hash from a remote registry
	return "hash456", nil
}
