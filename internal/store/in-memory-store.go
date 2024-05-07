package store

import (
	"context"
	"fmt"
	"strings"
)

type Image struct {
	Digest    string
	Reference string
}

type inMemoryStore struct {
	images  map[string]string
	fetcher ImageFetcher
}

type Storer interface {
	List(ctx context.Context) ([]Image, error)
	Add(ctx context.Context, digest string, image string) error
	Remove(ctx context.Context, digest string, image string) error
}

type ImageFetcher interface {
	List(ctx context.Context) ([]Image, error)
	ListDigests(ctx context.Context) ([]string, error)
	GetDigest(ctx context.Context, tag string) (string, error)
	GetTag(ctx context.Context, digest string) (string, error)
	Type() string
}

func NewInMemoryStore(fetcher ImageFetcher) Storer {
	return &inMemoryStore{
		images:  make(map[string]string),
		fetcher: fetcher,
	}
}

func (s *inMemoryStore) List(ctx context.Context) ([]Image, error) {
	var imageList []Image

	// Fetch images from the file/remote source
	imageList, err := s.fetcher.List(ctx)
	if err != nil {
		return nil, err
	}

	// Trim the imageList elements to remove the project name from the image reference
	for i, img := range imageList {
		parts := strings.Split(img.Reference, "/")
		if len(parts) > 1 {
			// Take the second part as the new Reference
			imageList[i].Reference = parts[1]
		}
	}

	// Since remote fetcher is based on tags retrieved via API and file fetcher must be able to handle a images without tags, they need to be handled separately
	// File fetcher will work based on digests instead of tags
	switch s.fetcher.Type() {
	case "File":

		// Iterate over imageList and call GetTag for each digest
		for _, img := range imageList {
			// Get the tag for the digest
			tag, err := s.fetcher.GetTag(ctx, img.Digest)
			if err != nil {
				return nil, err
			}
			// Check if the digest exists and matches the image reference
			// If the digest exists and does not match the image, update the store
			// If the digest does not exist, add it to the store
			s.checkDigestAndImage(img.Digest, tag)
		}

		// Create a map to keep track of the digests found in imageList
		digestMap := make(map[string]bool)

		// Iterate over imageList and add each digest to the map
		for _, img := range imageList {
			digestMap[img.Digest] = true
		}

		// Iterate over the stored list and delete any digest and its image that aren't found in imageList
		for digest := range s.images {
			if _, exists := digestMap[digest]; !exists {
				// The digest does not exist in imageList, so remove it from the store
				s.Remove(ctx, digest, s.images[digest])
			}
		}

	case "Remote":
		// iterate over imageList and call GetDigest for each tag
		for _, img := range imageList {
			// Split the image reference to get the tag
			tagParts := strings.Split(img.Reference, ":")
			// Check if there is a tag part, min length is 1 char
			if len(tagParts) < 2 {
				fmt.Println("No tag part found in the image reference")
			}
			// Use the last part as the tag
			tag := tagParts[len(tagParts)-1]
			// Get the digest for the tag
			digest, err := s.fetcher.GetDigest(ctx, tag)
			fmt.Printf("Digest for tag \"%s\" is %s\n", tag, digest)
			if err != nil {
				return nil, err
			}

			// Check if the image exists and matches the digest
			// If the image exists and does not match the digest, update the store
			// If the image does not exist, add it to the store
			s.checkImageAndDigest(digest, img.Reference)

		}

		// Create imageMap filled with all images from imageList
		imageMap := make(map[string]bool)
		for _, img := range imageList {
			imageMap[img.Reference] = true
		}

		// Iterate over in memory store and remove any image that is not found in imageMap
		for digest, image := range s.images {
			if _, exists := imageMap[image]; !exists {
				// The image does not exist in imageList, so remove it from the store
				s.Remove(ctx, digest, image)
			}
		}

	}

	// Print out the entire store
	fmt.Println("Current store:")
	for digest, imageRef := range s.images {
		fmt.Printf("Digest: %s, Image: %s\n", digest, imageRef)
	}

	return imageList, nil

}

func (s *inMemoryStore) Add(ctx context.Context, digest string, image string) error {
	// Add the image and its digest to the store
	s.images[digest] = image
	fmt.Printf("Added image: %s, digest: %s\n", image, digest)
	return nil
}

func (s *inMemoryStore) Remove(ctx context.Context, digest string, image string) error {
	// Remove the image and its digest from the store
	delete(s.images, digest)
	fmt.Printf("Removed image: %s, digest: %s\n", image, digest)
	return nil
}

// checkImageAndDigest checks if the image exists in the store and if the digest matches the image reference
func (s *inMemoryStore) checkImageAndDigest(digest string, image string) bool {
	// Check if the received image exists in the store
	for _, existingImage := range s.images {
		if existingImage == image {
			// Image exists, now check if the digest matches
			existingDigest, exists := s.images[digest]
			if exists && existingDigest == image {
				// Digest exists and matches the current image reference
				fmt.Printf("Digest for image %s exists in the store and matches\n", image)
				return true
			} else {
				// Digest exists but does not match the current image reference
				fmt.Printf("Digest for image %s exists in the store but does not match the current image reference\n", image)
				s.Remove(context.Background(), existingDigest, existingImage)
				s.Add(context.Background(), digest, image)
				return false
			}
		}
	}

	// If the image doesn't exist in the store, add it
	fmt.Printf("Image \"%s\" does not exist in the store\n", image)
	s.Add(context.Background(), digest, image)
	return false
}

// checkDigestAndImage checks if the digest exists in the store and if the corresponding image matches its digest
func (s *inMemoryStore) checkDigestAndImage(digest string, image string) bool {
	// Check if the received digest exists in the store
	for existingDigest, existingImage := range s.images {
		if existingDigest == digest {
			// Digest exists, now check if the corresponding image matches
			if existingImage == image {
				// Image exists and the corresponding digest matches
				return true
			} else {
				// Image exists but the corresponding digest does not match
				fmt.Printf("Digest %s exists in the store but does not match the image %s\n", digest, image)
				s.Remove(context.Background(), existingDigest, existingImage)
				s.Add(context.Background(), digest, image)
				return false
			}
		}
	}

	// If the digest doesn't exist in the store, add it
	s.Add(context.Background(), digest, image)
	return false
}
