// sync.Map version for high concurrency handling but slight performance degradation
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type Image struct {
	Reference string
}

type Storer interface {
	List(ctx context.Context) ([]Image, error)
	Add(ctx context.Context, img Image) error
	Remove(ctx context.Context, ref string) error
}

type inMemoryStore struct {
	images sync.Map
}

func NewInMemoryStore() Storer {
	return &inMemoryStore{}
}

func (s *inMemoryStore) List(ctx context.Context) ([]Image, error) {
	var images []Image
	s.images.Range(func(key, value interface{}) bool {
		images = append(images, value.(Image))
		return true
	})

	// Fetch the current list of images from the external source
	fetcher := NewImageListFetcher()
	fetchedImages, err := fetcher.List(ctx)
	if err != nil {
		return nil, err
	}

	// Check if the fetched images are the same as the locally stored images
	if len(images) == 0 || !areImagesEqual(images, fetchedImages) {
		// If the local store is empty or the images are different, update the local store
		fmt.Println("Updating local store with new images...")
		s.images.Range(func(key, value interface{}) bool {
			s.images.Delete(key) // Clear the local store
			return true
		})
		for _, img := range fetchedImages {
			s.Add(ctx, img) // Add the new images to the local store
		}
		// Reload images from the store
		s.images.Range(func(key, value interface{}) bool {
			images = append(images, value.(Image))
			return true
		})
	}

	return images, nil
}

func (s *inMemoryStore) Add(ctx context.Context, img Image) error {
	s.images.Store(img.Reference, img)
	return nil
}

func (s *inMemoryStore) Remove(ctx context.Context, ref string) error {
	s.images.Delete(ref)
	return nil
}

func areImagesEqual(localImages, remoteImages []Image) bool {
	fmt.Println("Comparing local and remote images...")
	fmt.Println("Local images:", localImages)
	fmt.Println("Remote images:", remoteImages)

	localSet := make(map[string]bool)
	remoteSet := make(map[string]bool)

	// Populate the set for local images
	for _, img := range localImages {
		localSet[img.Reference] = true
	}

	// Populate the set for remote images
	for _, img := range remoteImages {
		remoteSet[img.Reference] = true
	}

	// Check if both sets are identical
	if len(localSet) != len(remoteSet) {
		fmt.Println("The number of unique images does not match.")
		return false
	}

	for key := range localSet {
		// fmt.Println("Checking image:", key)
		if !remoteSet[key] {
			fmt.Printf("Image %s found in local images but not in remote images\n", key)
			return false
		}
	}

	fmt.Println("All images match.")
	return true
}

type ImageListFetcher struct {
	BaseURL    string
	Repository string
}

func NewImageListFetcher() *ImageListFetcher {
	return &ImageListFetcher{
		BaseURL:    "https://registry.hub.docker.com/v2/repositories",
		Repository: "alpine",
	}
}

func (client *ImageListFetcher) List(ctx context.Context) ([]Image, error) {
	url := fmt.Sprintf("%s/%s/", client.BaseURL, client.Repository)
	fmt.Println("URL :", url)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch images: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}

	err = json.Unmarshal(body, &data)
	if err != nil {
		return nil, err
	}

	images := make([]Image, len(data.Results))
	for i, result := range data.Results {
		images[i] = Image{
			Reference: fmt.Sprintf("%s:%s", client.Repository, result.Name),
		}
	}
	fmt.Println("Fetched", len(images), "images :", images)

	return images, nil
}

// Below version is for low concurrency handling and slightly better performance
/* package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type Image struct {
	Reference string
}

type Storer interface {
	List(ctx context.Context) ([]Image, error)
	Add(ctx context.Context, img Image) error
	Remove(ctx context.Context, ref string) error
}

type inMemoryStore struct {
	images map[string]Image
	mu     sync.RWMutex
}

func NewInMemoryStore() Storer {
	return &inMemoryStore{
		images: make(map[string]Image),
	}
}

func (s *inMemoryStore) List(ctx context.Context) ([]Image, error) {
	fmt.Println("In Memory Store List")
	var images []Image
	s.mu.RLock() // Lock for reading
	for _, img := range s.images {
		images = append(images, img)
	}
	s.mu.RUnlock() // Unlock after reading

	// Fetch the current list of images from the external source
	fetcher := NewImageListFetcher()
	fetchedImages, err := fetcher.List(ctx)
	if err != nil {
		return nil, err
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
		s.mu.RLock() // Lock for reading
		for _, img := range s.images {
			images = append(images, img)
		}
		fmt.Println("Images after update:", images)
		s.mu.RUnlock() // Unlock after reading
	} else {
		// If the images are the same, inform the user
		fmt.Println("Images already present in store and are up to date.")
	}
	fmt.Println("Images after comparison:", images)
	return images, nil
}

func (s *inMemoryStore) Add(ctx context.Context, img Image) error {
	s.mu.Lock() // Lock for writing
	s.images[img.Reference] = img
	s.mu.Unlock() // Unlock after writing
	return nil
}

func (s *inMemoryStore) Remove(ctx context.Context, ref string) error {
	s.mu.Lock() // Lock for writing
	delete(s.images, ref)
	s.mu.Unlock() // Unlock after writing
	return nil
}

func areImagesEqual(localImages, remoteImages []Image) bool {
	fmt.Println("Comparing images...")
	fmt.Println("Local images:", localImages)
	fmt.Println("Remote images:", remoteImages)

	localSet := make(map[string]bool)
	remoteSet := make(map[string]bool)

	// Populate the set for local images
	for _, img := range localImages {
		localSet[img.Reference] = true
	}

	// Populate the set for remote images
	for _, img := range remoteImages {
		remoteSet[img.Reference] = true
	}

	// Check if both sets are identical
	if len(localSet) != len(remoteSet) {
		fmt.Println("The number of unique images does not match.")
		return false
	}

	for key := range localSet {
		if !remoteSet[key] {
			fmt.Printf("Image %s found in local images but not in remote images\n", key)
			return false
		}
	}

	fmt.Println("All images match.")
	return true
}

type ImageListFetcher struct {
	BaseURL    string
	Repository string
}

func NewImageListFetcher() *ImageListFetcher {
	return &ImageListFetcher{
		BaseURL:    "https://registry.hub.docker.com/v2/repositories",
		Repository: "alpine",
	}
}

func (client *ImageListFetcher) List(ctx context.Context) ([]Image, error) {
	url := fmt.Sprintf("%s/%s/", client.BaseURL, client.Repository)
	fmt.Println("URL :", url)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch images: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}

	err = json.Unmarshal(body, &data)
	if err != nil {
		return nil, err
	}

	images := make([]Image, len(data.Results))
	for i, result := range data.Results {
		images[i] = Image{
			Reference: fmt.Sprintf("%s:%s", client.Repository, result.Name),
		}
	}
	fmt.Println("Fetched", len(images), "images :", images)

	return images, nil
} */
