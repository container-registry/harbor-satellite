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
		{Reference: "alpine:3.12"},
		{Reference: "alpine:3.11"},
		{Reference: "alpine:3.10"},
	}
	return images, nil
}
