package store

import (
	"context"
	"errors"
	"fmt"
)

type Image struct {
	Reference string
}

type inMemoryStore struct {
	images  map[string][]Image
	fetcher ImageFetcher
}

type Storer interface {
	List(ctx context.Context) ([]Image, error)
	Add(ctx context.Context, hash string, imageList []Image) error
	Remove(ctx context.Context, hash string) error
}

type ImageFetcher interface {
	List(ctx context.Context) ([]Image, error)
	GetHash(ctx context.Context) (string, error)
}

func NewInMemoryStore(fetcher ImageFetcher) Storer {
	return &inMemoryStore{
		images:  make(map[string][]Image),
		fetcher: fetcher,
	}
}

func (s *inMemoryStore) List(ctx context.Context) ([]Image, error) {
	// Check if the local store is empty
	if len(s.images) == 0 {
		fmt.Println("Local store is empty. Fetching images from the remote source...")
		// Fetch images from the remote source
		imageList, err := s.fetcher.List(ctx)
		if err != nil {
			return nil, err
		}

		// Fetch the remote hash
		remoteHash, err := s.fetcher.GetHash(ctx)
		if err != nil {
			return nil, err
		}

		// Add the fetched images and hash to the local store
		fmt.Println("Adding fetched images and hash to the local store...")
		s.Add(ctx, remoteHash, imageList)
	} else {
		fmt.Println("Local store is not empty. Fetching images from external source...")

		imageList, err := s.fetcher.List(ctx)
		if err != nil {
			return nil, err
		}

		remoteHash, err := s.fetcher.GetHash(ctx)
		if err != nil {
			return nil, err
		}

		localHash, err := s.GetLocalHash(ctx)
		if err != nil {
			return nil, err
		}

		// If the local and remote hashes are not equal, add the new images to the store
		if !areImagesEqual(localHash, remoteHash) {
			fmt.Println("Local and remote hashes are not equal. Updating the local store with new images...")
			s.Remove(ctx, "")
			s.Add(ctx, remoteHash, imageList)
		} else {
			fmt.Println("Local and remote hashes are equal. No update needed.")
		}
	}
	var allImages []Image
	for _, images := range s.images {
		allImages = append(allImages, images...)
	}
	return allImages, nil
}

func (s *inMemoryStore) Add(ctx context.Context, hash string, imageList []Image) error {
	s.images[hash] = imageList
	fmt.Println("Images and hash added to the store:", s.images)
	return nil
}

// Remove images from the store based on the provided hash
// If no hash is provided, it clears the entire store
func (s *inMemoryStore) Remove(ctx context.Context, hash string) error {
	if hash == "" {
		s.images = make(map[string][]Image)
		fmt.Println("Store cleared.")
	} else {
		fmt.Println("Removing images with hash:", hash)
		delete(s.images, hash)
	}
	return nil
}

func areImagesEqual(localHash string, remoteHash string) bool {
	fmt.Println("Local hash:", localHash)
	fmt.Println("Remote hash:", remoteHash)
	return localHash == remoteHash
}

func (s *inMemoryStore) GetLocalHash(ctx context.Context) (string, error) {
	for hash, images := range s.images {
		if len(images) > 0 {
			return hash, nil
		}
	}
	return "", errors.New("no hash found in the local store")
}
