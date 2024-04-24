package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type Image struct {
	Reference string
}

type Storer interface {
	List(ctx context.Context) ([]Image, error)
	Add(ctx context.Context, img Image) error
	Remove(ctx context.Context, ref string) error
}

type ImageFetcher interface {
	List(ctx context.Context) ([]Image, error)
}

type inMemoryStore struct {
	images  map[string]Image
	fetcher ImageFetcher
}

func NewInMemoryStore(fetcher ImageFetcher) Storer {
	return &inMemoryStore{
		images:  make(map[string]Image),
		fetcher: fetcher,
	}
}

func (s *inMemoryStore) List(ctx context.Context) ([]Image, error) {
	fetchedImages, err := s.fetcher.List(ctx)
	if err != nil {
		return nil, err
	}
	var images []Image
	for _, img := range s.images {
		images = append(images, img)
	}

	// Check if the fetched images are the same as the locally stored images
	if len(images) == 0 || !areImagesEqual(images, fetchedImages) {
		// If the local store is empty or the images are different, update the local store
		fmt.Println("Updating local store with new images...")
		for k := range s.images {
			fmt.Println("Deleting image:", k)
			s.Remove(ctx, k) // Clear the local store
		}
		for _, img := range fetchedImages {
			fmt.Println("Adding image:", img)
			s.Add(ctx, img) // Add the new images to the local store
		}
		// Reload images from the store
		for _, img := range s.images {
			images = append(images, img)
		}
		fmt.Println("Images after update:", images)
	} else {
		// If the images are the same, inform the user
		fmt.Println("Images present in store are up to date.")
	}
	return fetchedImages, nil
}

func (s *inMemoryStore) Add(ctx context.Context, img Image) error {
	s.images[img.Reference] = img
	return nil
}

func (s *inMemoryStore) Remove(ctx context.Context, ref string) error {
	delete(s.images, ref)
	return nil
}

func areImagesEqual(localImages, fetchedImages []Image) bool {
	hashMap1 := make(map[string]Image)
	hashMap2 := make(map[string]Image)

	for _, img := range localImages {
		hash := img.Hash()
		hashMap1[hash] = img
		fmt.Printf("localImages Hash: %s\n", hash)
	}

	for _, img := range fetchedImages {
		hash := img.Hash()
		hashMap2[hash] = img
		fmt.Printf("fetchedImages Hash: %s\n", hash)
	}

	if len(hashMap1) != len(hashMap2) {
		return false
	}

	for k, v := range hashMap1 {
		if v2, ok := hashMap2[k]; !ok || v != v2 {
			return false
		}
	}

	return true
}

func (img Image) Hash() string {
	hash := sha256.Sum256([]byte(img.Reference))
	return hex.EncodeToString(hash[:])
}
