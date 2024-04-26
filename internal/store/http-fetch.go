package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type RemoteImageList struct {
	BaseURL    string
	Repository string
}

func RemoteImageListFetcher() *RemoteImageList {
	return &RemoteImageList{
		BaseURL:    "https://registry.hub.docker.com/v2/repositories",
		Repository: "alpine",
	}
}

func (client *RemoteImageList) List(ctx context.Context) ([]Image, error) {
	url := fmt.Sprintf("%s/%s/", client.BaseURL, client.Repository)
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

func (client *RemoteImageList) GetHash(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/%s/", client.BaseURL, client.Repository)
	fmt.Println("Source :", url)
	resp, err := http.Get(url)
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
