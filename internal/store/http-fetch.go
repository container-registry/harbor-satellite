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
	"time"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/logger"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

type RemoteImageList struct {
	BaseURL      string
	username     string
	password     string
	use_unsecure bool
	zot_url      string
}

type TagListResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func RemoteImageListFetcher(ctx context.Context, url string) *RemoteImageList {
	return &RemoteImageList{
		BaseURL:      url,
		username:     config.GetHarborUsername(),
		password:     config.GetHarborPassword(),
		use_unsecure: config.UseUnsecure(),
		zot_url:      config.GetZotURL(),
	}
}

func (r *RemoteImageList) Type(ctx context.Context) string {
	return "Remote"
}

func (client *RemoteImageList) List(ctx context.Context) ([]Image, error) {
	log := logger.FromContext(ctx)
	// Construct the URL for fetching tags
	url := client.BaseURL + "/tags/list"

	// Encode credentials for Basic Authentication
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	// Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Error().Msgf("failed to create request: %v", err)
		return nil, err
	}

	// Set the Authorization header
	req.Header.Set("Authorization", "Basic "+auth)

	// Configure the HTTP client with a timeout
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Send the HTTP request
	log.Info().Msgf("Sending request to %s", url)
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Error().Msgf("failed to send request: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Msgf("failed to read response body: %v", err)
		return nil, err
	}

	// Unmarshal the JSON response
	var tagListResponse TagListResponse
	if err := json.Unmarshal(body, &tagListResponse); err != nil {
		log.Error().Msgf("failed to unmarshal response: %v", err)
		return nil, err
	}

	// Prepare a slice to store the images
	var images []Image

	// Iterate over the tags and construct the image references
	for _, tag := range tagListResponse.Tags {
		images = append(images, Image{
			Name: fmt.Sprintf("%s:%s", tagListResponse.Name, tag),
		})
	}
	return images, nil
}

func (client *RemoteImageList) GetDigest(ctx context.Context, tag string) (string, error) {
	log := logger.FromContext(ctx)
	// Construct the image reference
	imageRef := fmt.Sprintf("%s:%s", client.BaseURL, tag)
	// Remove extra characters from the URL
	imageRef = imageRef[strings.Index(imageRef, "//")+2:]
	imageRef = strings.ReplaceAll(imageRef, "/v2", "")

	// Encode credentials for Basic Authentication
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	auth := &authn.Basic{Username: username, Password: password}
	// Prepare options for crane.Digest
	options := []crane.Option{crane.WithAuth(auth)}
	if client.use_unsecure {
		options = append(options, crane.Insecure)
	}

	// Use crane.Digest to get the digest of the image
	digest, err := crane.Digest(imageRef, options...)
	if err != nil {
		log.Error().Msgf("failed to get digest using crane: %v", err)
		return "", err
	}

	return digest, nil
}
