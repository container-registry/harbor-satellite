package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
)

type RemoteImageList struct {
	BaseURL string
}

func RemoteImageListFetcher(url string) *RemoteImageList {
	return &RemoteImageList{
		BaseURL: url,
	}
}

func (client *RemoteImageList) List(ctx context.Context) ([]Image, error) {
	// Extract the last segment of the BaseURL to use as the image name
	lastSegment := path.Base(client.BaseURL)
	fmt.Println("Last segment:", lastSegment)
	resp, err := http.Get(client.BaseURL)
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
			Reference: fmt.Sprintf("%s:%s", lastSegment, result.Name),
		}
	}
	fmt.Println("Fetched", len(images), "images :", images)

	return images, nil
}

func (client *RemoteImageList) GetHash(ctx context.Context) (string, error) {
	resp, err := http.Get(client.BaseURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch images: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Hash and return the body
	hash := sha256.Sum256(body)
	hashString := hex.EncodeToString(hash[:])

	return hashString, nil
}
