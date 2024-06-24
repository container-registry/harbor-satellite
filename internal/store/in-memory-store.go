package store

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
)

type Image struct {
	Digest string
	Name   string
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
	GetDigest(ctx context.Context, tag string) (string, error)
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
	var change bool

	// Fetch images from the file/remote source
	imageList, err := s.fetcher.List(ctx)
	if err != nil {
		return nil, err
	}

	// Handle File and Remote fetcher types differently
	switch s.fetcher.Type() {
	case "File":
		for _, img := range imageList {
			// Check if the image already exists in the store
			if _, exists := s.images[img.Name]; !exists {
				// Add the image to the store
				s.AddImage(ctx, img.Name)
				change = true
			} else {
				fmt.Printf("Image %s already exists in the store\n", img.Name)
			}
		}

		// Iterate over s.images and remove any image that is not found in imageList
		for image := range s.images {
			found := false
			for _, img := range imageList {
				if img.Name == image {
					found = true
					break
				}
			}
			if !found {
				s.RemoveImage(ctx, image)
				change = true
			}
		}

		// Empty and refill imageList with the contents from s.images
		imageList = imageList[:0]
		for name, digest := range s.images {
			imageList = append(imageList, Image{Name: name, Digest: digest})
		}

		// Print out the entire store for debugging purposes
		fmt.Println("Current store:")
		for image := range s.images {
			fmt.Printf("Image: %s\n", image)
		}

	case "Remote":
		// Trim the imageList elements to remove the project name from the image reference
		for i, img := range imageList {
			parts := strings.Split(img.Name, "/")
			if len(parts) > 1 {
				// Take the second part as the new Reference
				imageList[i].Name = parts[1]
			}
		}
		// iterate over imageList and call GetDigest for each tag
		for _, img := range imageList {
			// Split the image reference to get the tag
			tagParts := strings.Split(img.Name, ":")
			// Check if there is a tag part, min length is 1 char
			if len(tagParts) < 2 {
				fmt.Println("No tag part found in the image reference")
			}
			// Use the last part as the tag
			tag := tagParts[len(tagParts)-1]
			// Get the digest for the tag
			digest, err := s.fetcher.GetDigest(ctx, tag)
			if err != nil {
				return nil, err
			}

			// Check if the image exists and matches the digest
			if !(s.checkImageAndDigest(digest, img.Name)) {
				change = true
			}

		}

		// Create imageMap filled with all images from remote imageList
		imageMap := make(map[string]bool)
		for _, img := range imageList {
			imageMap[img.Name] = true
		}

		// Iterate over in memory store and remove any image that is not found in imageMap
		for digest, image := range s.images {
			if _, exists := imageMap[image]; !exists {
				s.Remove(ctx, digest, image)
				change = true
			}
		}
		// Print out the entire store for debugging purposes
		fmt.Println("Current store:")
		for digest, imageRef := range s.images {
			fmt.Printf("Digest: %s, Image: %s\n", digest, imageRef)
		}

		// Empty and refill imageList with the contents from s.images
		imageList = imageList[:0]
		for _, name := range s.images {
			imageList = append(imageList, Image{Digest: "", Name: name})
		}

	}
	if change {
		fmt.Println("Changes detected in the store")
		change = false
		return imageList, nil
	} else {
		fmt.Println("No changes detected in the store")
		return nil, nil
	}
}

func (s *inMemoryStore) Add(ctx context.Context, digest string, image string) error {
	// Check if the image already exists in the store
	if _, exists := s.images[digest]; exists {
		fmt.Printf("Image: %s, digest: %s already exists in the store.\n", image, digest)
		return fmt.Errorf("image %s already exists in the store", image)
	} else {
		// Add the image and its digest to the store
		s.images[digest] = image
		fmt.Printf("Successfully added image: %s, digest: %s\n", image, digest)
		return nil
	}
}

func (s *inMemoryStore) AddImage(ctx context.Context, image string) error {
	// Add the image to the store
	s.images[image] = ""
	fmt.Printf("Added image: %s\n", image)
	return nil
}

func (s *inMemoryStore) Remove(ctx context.Context, digest string, image string) error {
	// Check if the image exists in the store
	if _, exists := s.images[digest]; exists {
		// Remove the image and its digest from the store
		delete(s.images, digest)
		fmt.Printf("Successfully removed image: %s, digest: %s\n", image, digest)
		return nil
	} else {
		fmt.Printf("Failed to remove image: %s, digest: %s. Not found in the store.\n", image, digest)
		return fmt.Errorf("image %s not found in the store", image)
	}
}

func (s *inMemoryStore) RemoveImage(ctx context.Context, image string) error {
	// Remove the image from the store
	delete(s.images, image)
	fmt.Printf("Removed image: %s\n", image)
	return nil
}

// TODO: Rework complicated logic and add support for multiple repositories
// checkImageAndDigest checks if the image exists in the store and if the digest matches the image reference
func (s *inMemoryStore) checkImageAndDigest(digest string, image string) bool {
	// Check if the received image exists in the store
	for storeDigest, storeImage := range s.images {
		if storeImage == image {
			// Image exists, now check if the digest matches
			if storeDigest == digest {
				// Digest exists and matches the current image's
				// Remove what comes before the ":" in image and set it to tag variable
				tag := strings.Split(image, ":")[1]
				localRegistryDigest, err := GetLocalDigest(context.Background(), tag)
				if err != nil {
					fmt.Println("Error getting digest from local registry:", err)
					return false
				} else {
					// Check if the digest from the local registry matches the digest from the store
					if digest == localRegistryDigest {
						return true
					} else {
						return false
					}
				}
			} else {
				// Digest exists but does not match the current image reference
				s.Remove(context.Background(), storeDigest, storeImage)
				s.Add(context.Background(), digest, image)
				return false
			}
		}
	}

	// Try to add the image to the store
	// Add will check if it already exists in the store before adding
	// If adding was successful, return true, else return false
	err := s.Add(context.Background(), digest, image)
	return err != nil
}

func GetLocalDigest(ctx context.Context, tag string) (string, error) {
	zotUrl := os.Getenv("ZOT_URL")
	userURL := os.Getenv("USER_INPUT")
	// Remove extra characters from the URLs
	userURL = userURL[strings.Index(userURL, "//")+2:]
	userURL = strings.ReplaceAll(userURL, "/v2", "")

	regUrl := removeHostName(userURL)
	// Construct the URL for fetching the digest
	url := zotUrl + "/" + regUrl + ":" + tag

	// Use crane.Digest to get the digest of the image
	digest, err := crane.Digest(url)
	if err != nil {
		return "", fmt.Errorf("failed to get digest using crane: %w", err)
	}

	return digest, nil
}

// Split the imageName by "/" and take only the parts after the hostname
func removeHostName(imageName string) string {
	parts := strings.Split(imageName, "/")
	if len(parts) > 1 {
		return strings.Join(parts[1:], "/")
	}

	return imageName
}
