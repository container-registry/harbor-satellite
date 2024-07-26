package store

import (
	"context"
	"fmt"
	"log"
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
	Add(ctx context.Context, digest, image string) error
	Remove(ctx context.Context, digest, image string) error
}

type ImageFetcher interface {
	List(ctx context.Context) ([]Image, error)
	GetDigest(ctx context.Context, tag string) (string, error)
	SourceType() string
}

func NewInMemoryStore(fetcher ImageFetcher) Storer {
	return &inMemoryStore{
		images:  make(map[string]string),
		fetcher: fetcher,
	}
}

// List retrieves and synchronizes the list of images
func (s *inMemoryStore) List(ctx context.Context) ([]Image, error) {
	// fetch List of images
	imageList, err := s.fetcher.List(ctx)
	if err != nil {
		return nil, err
	}

	var changeDetected bool

	switch s.fetcher.SourceType() {
	case "File":
		changeDetected, err = s.handleFileSource(ctx, imageList)
	case "Remote":
		changeDetected, err = s.handleRemoteSource(ctx, imageList)
	default:
		return nil, fmt.Errorf("unknown source type")
	}
	if err != nil {
		return nil, err
	}

	if changeDetected {
		fmt.Println("Changes detected in  store")
		return s.getImageList(), nil
	} else {
		fmt.Println("No changes detected in the store")
		return nil, nil
	}
}

// handleFileSource handles image from a file
func (s *inMemoryStore) handleFileSource(ctx context.Context, imageList []Image) (bool, error) {
	var change bool
	for _, img := range imageList {
		// Check if  image already exists in  store
		if _, exists := s.images[img.Name]; !exists {
			// Add  image to  store
			s.AddImage(ctx, img.Name)
			change = true
		} else {
			fmt.Printf("Image %s already exists in  store\n", img.Name)
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
			if err := s.RemoveImage(ctx, image); err != nil {
				return false, err
			}
			change = true
		}
	}

	// Empty and refill imageList with  contents from s.images
	imageList = imageList[:0]
	for name, digest := range s.images {
		imageList = append(imageList, Image{Name: name, Digest: digest})
	}

	// Print out  entire store for debugging purposes
	fmt.Println("Current store:")
	for image := range s.images {
		fmt.Printf("Image: %s\n", image)
	}

	return change, nil
}

// handleRemoteSource handles images fetched from a remote source.
func (s *inMemoryStore) handleRemoteSource(ctx context.Context, imageList []Image) (bool, error) {
	var change bool
	// Trim the imageList elements to remove  project name from  image reference
	for i, img := range imageList {
		parts := strings.Split(img.Name, "/")
		if len(parts) > 1 {
			// Take  second part as  new Reference
			imageList[i].Name = parts[1]
		}
	}
	// iterate over imageList and call GetDigest for each tag
	for _, img := range imageList {
		// Split  image reference to get  tag
		tagParts := strings.Split(img.Name, ":")
		// Check if there is a tag part, min length is 1 char
		if len(tagParts) < 2 {
			fmt.Println("No tag part found in  image reference")
		}
		// Use  last part as  tag
		tag := tagParts[len(tagParts)-1]
		// Get  digest for  tag
		digest, err := s.fetcher.GetDigest(ctx, tag)
		if err != nil {
			return false, err
		}

		// Check if  image exists and matches the digest
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
			if err := s.Remove(ctx, digest, image); err != nil {
				return false, err
			}

			change = true
		}
	}
	// Print out the entire store for debugging purposes
	fmt.Println("Current store:")
	for digest, imageRef := range s.images {
		fmt.Printf("Digest: %s, Image: %s\n", digest, imageRef)
	}

	// Empty and refill imageList with  contents from s.images
	imageList = imageList[:0]
	for _, name := range s.images {
		imageList = append(imageList, Image{Digest: "", Name: name})
	}

	return change, nil
}

// getImageList converts the in-memory store to a list of Image structs.
func (s *inMemoryStore) getImageList() []Image {
	var imageList []Image
	// Empty and refill imageList with  contents from s.images
	for _, name := range s.images {
		imageList = append(imageList, Image{Digest: "", Name: name})
	}
	return imageList
}

// Add image and its digest to  store
func (s *inMemoryStore) Add(ctx context.Context, digest string, image string) error {
	// Check if  image already exists in  store
	if _, exists := s.images[digest]; exists {
		fmt.Printf("Image: %s, digest: %s already exists in  store.\n", image, digest)
		return fmt.Errorf("image %s already exists in  store", image)
	} else {
		s.images[digest] = image
		fmt.Printf("Successfully added image: %s, digest: %s\n", image, digest)
		return nil
	}
}

// Add image to  store
func (s *inMemoryStore) AddImage(ctx context.Context, image string) {
	if _, exists := s.images[image]; exists {
		fmt.Printf(
			"Warning: Image %s already exists in  store. Proceeding with the addition.\n",
			image,
		)
	}
	s.images[image] = ""
	fmt.Printf("Added image: %s\n", image)
}

// Removes image from store
func (s *inMemoryStore) Remove(ctx context.Context, digest string, image string) error {
	// Check if  image exists in  store
	if _, exists := s.images[digest]; exists {
		// Remove  image and its digest from  store
		delete(s.images, digest)
		fmt.Printf("Successfully removed image: %s, digest: %s\n", image, digest)
		return nil
	} else {
		fmt.Printf("Failed to remove image: %s, digest: %s. Not found in the store.\n", image, digest)
		return fmt.Errorf("image %s not found in the store", image)
	}
}

// Remove  image from  store
func (s *inMemoryStore) RemoveImage(ctx context.Context, image string) error {
	if _, exists := s.images[image]; !exists {
		return fmt.Errorf("image %s not found in the store", image)
	}
	delete(s.images, image)
	fmt.Printf("Removed image: %s\n", image)
	return nil
}

// TODO: Rework complicated logic and add support for multiple repositories
// checkImageAndDigest checks if  image exists in  store and if the digest matches the image reference
func (s *inMemoryStore) checkImageAndDigest(digest string, image string) bool {
	// Check if  received image exists in  store
	for storeDigest, storeImage := range s.images {
		if storeImage == image {
			// Image exists, now check if  digest matches
			if storeDigest == digest {
				// Digest exists and matches  current image's
				// Remove what comes before  ":" in image and set it to tag variable
				tag := strings.Split(image, ":")[1]
				localRegistryDigest, err := GetLocalDigest(context.Background(), tag)
				if err != nil {
					fmt.Println("Error getting digest from local registry:", err)
					return false
				} else {
					// Check if  digest from  local registry matches  digest from  store
					if digest == localRegistryDigest {
						return true
					} else {
						return false
					}
				}
			} else {
				// Digest exists but does not match  current image reference
				if err := s.Remove(context.Background(), storeDigest, storeImage); err != nil {
					log.Println("Error: %w", err)
					return false
				}

				if err := s.Add(context.Background(), digest, image); err != nil {
					log.Fatalf("Error in adding image to store: %v", err)
				}
				return false
			}
		}
	}

	// Try to add  image to  store
	// Add will check if it already exists in  store before adding
	// If adding was successful, return true, else return false
	err := s.Add(context.Background(), digest, image)
	return err != nil
}

func GetLocalDigest(ctx context.Context, tag string) (string, error) {
	zotUrl := os.Getenv("ZOT_URL")
	userURL := os.Getenv("USER_INPUT")
	// Remove extra characters from  URLs
	userURL = userURL[strings.Index(userURL, "//")+2:]
	userURL = strings.ReplaceAll(userURL, "/v2", "")

	regUrl := removeHostName(userURL)
	// Construct  URL for fetching  digest
	url := zotUrl + "/" + regUrl + ":" + tag

	// Use crane.Digest to get  digest of  image
	digest, err := crane.Digest(url)
	if err != nil {
		return "", fmt.Errorf("failed to get digest using crane: %w", err)
	}

	return digest, nil
}

// Split  imageName by "/" and take only  parts after  hostname
func removeHostName(imageName string) string {
	parts := strings.Split(imageName, "/")
	if len(parts) > 1 {
		return strings.Join(parts[1:], "/")
	}

	return imageName
}
