package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type RemoteImageList struct {
	BaseURL string
}

type TagListResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func RemoteImageListFetcher(url string) *RemoteImageList {
	return &RemoteImageList{
		BaseURL: url,
	}
}

func (r *RemoteImageList) Type() string {
	return "Remote"
}

func (client *RemoteImageList) List(ctx context.Context) ([]Image, error) {
	// Construct the URL for fetching tags
	url := client.BaseURL + "/tags/list"

	// Encode credentials for Basic Authentication
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	// Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set the Authorization header
	req.Header.Set("Authorization", "Basic "+auth)

	// Send the request
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Unmarshal the JSON response
	var tagListResponse TagListResponse
	if err := json.Unmarshal(body, &tagListResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON response: %w", err)
	}

	// Prepare a slice to store the images
	var images []Image

	// Iterate over the tags and construct the image references
	for _, tag := range tagListResponse.Tags {
		images = append(images, Image{
			Reference: fmt.Sprintf("%s:%s", tagListResponse.Name, tag),
		})
	}
	fmt.Println("Fetched", len(images), "images :", images)
	return images, nil
}

func (client *RemoteImageList) GetDigest(ctx context.Context, tag string) (string, error) {
	// Construct the URL for fetching the manifest
	url := client.BaseURL + "/manifests/" + tag

	// Encode credentials for Basic Authentication
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	// Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set the Authorization header
	req.Header.Set("Authorization", "Basic "+auth)

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

func (client *RemoteImageList) GetTag(ctx context.Context, digest string) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}

func (client *RemoteImageList) ListDigests(ctx context.Context) ([]string, error) {
	return nil, fmt.Errorf("not implemented yet")
}
