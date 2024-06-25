package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
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
			Name: fmt.Sprintf("%s:%s", tagListResponse.Name, tag),
		})
	}
	fmt.Println("Fetched", len(images), "images :", images)
	return images, nil
}

func (client *RemoteImageList) GetDigest(ctx context.Context, tag string) (string, error) {
	// Construct the image reference
	imageRef := fmt.Sprintf("%s:%s", client.BaseURL, tag)
	// Remove extra characters from the URL
	imageRef = imageRef[strings.Index(imageRef, "//")+2:]
	imageRef = strings.ReplaceAll(imageRef, "/v2", "")

	// Encode credentials for Basic Authentication
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")

	// Use crane.Digest to get the digest of the image
	digest, err := crane.Digest(imageRef, crane.WithAuth(&authn.Basic{
		Username: username,
		Password: password,
	}))
	if err != nil {
		fmt.Printf("failed to fetch digest for %s: %v\n", imageRef, err)
		return "", nil
	}

	return digest, nil
}
