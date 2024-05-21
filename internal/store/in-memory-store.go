package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
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

// checkImageAndDigest checks if the image exists in the store and if the digest matches the image reference
func (s *inMemoryStore) checkImageAndDigest(digest string, image string) bool {

	// Check if the received image exists in the store
	for storeDigest, storeImage := range s.images {
		if storeImage == image {
			// Image exists, now check if the digest matches
			if storeDigest == digest {
				// Digest exists and matches the current image's
				fmt.Printf("Digest for image %s exists in the store and matches remote digest\n", image)
				// TODO: Add support for multiple repositories
				// Remove what comes before the ":" in image and set it to tag cariable
				tag := strings.Split(image, ":")[1]
				localRegistryDigest, err := GetLocalDigest(context.Background(), tag)
				if err != nil {
					fmt.Println("Error getting digest from local registry:", err)
					return false
				} else {
					if digest == localRegistryDigest {
						fmt.Printf("Digest for image %s in the store matches the local registry\n", image)
						return true
					} else {
						fmt.Printf("Digest %s for image %s in the store does not match the local registry digest %s \n", storeDigest, image, localRegistryDigest)
						return false
					}
				}
			} else {
				// Digest exists but does not match the current image reference
				fmt.Printf("Digest for image %s exists in the store but does not match the current image reference\n", image)
				s.Remove(context.Background(), storeDigest, storeImage)
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

func GetLocalDigest(ctx context.Context, tag string) (string, error) {
	zotUrl := os.Getenv("ZOT_URL")
	// Construct the URL for fetching the manifest
	userURL := os.Getenv("USER_INPUT")
	// remove the starting elements until the double slash
	userURL = userURL[strings.Index(userURL, "//")+2:]
	userURL = strings.ReplaceAll(userURL, "/v2", "")

	// Construct the URL for fetching the manifest
	url := "http://" + zotUrl + "/v2/" + userURL + "/manifests/" + tag

	// Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set the Authorization header to accept correct manifest media type
	req.Header.Add("Accept", "application/vnd.oci.image.manifest.v1+json")

	// Send the request
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Unmarshal the JSON response
	var manifestResponse v1.Manifest
	if err := json.Unmarshal(body, &manifestResponse); err != nil {
		return "", fmt.Errorf("failed to unmarshal JSON response: %w", err)
	}

	// Return the digest from the config section of the response
	return string(manifestResponse.Config.Digest), nil
}
