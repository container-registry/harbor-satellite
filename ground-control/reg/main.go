package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

type Image struct {
	Digest string
	Name   string
}

type TagListResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func main() {
	fmt.Println("running registry fetcher..")
	ListRepos(context.Background())
	repos, err := fetchRepos("admin", "Harbor12345", "https://demo.goharbor.io")
	if err != nil {
		log.Fatalf("failed to fetchRepos: %v", repos)
	}

	log.Printf("These are \n repos found: %v", repos)
}

func fetchRepos(username, password, url string) ([]string, error) {
	// username := "admin"
	// password := "Harbor12345"
	// url := "https://demo.goharbor.io/v2/_catalog?n=1000"
	url = url + "/v2/_catalog?n=1000"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	// Encode credentials for Basic Authentication
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	req.Header.Set("Authorization", "Basic "+auth)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	fmt.Println("Response Status:", resp.Status)
	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch catalog: %s", resp.Status)
	}

	// Read the response body and decode JSON
	var result map[string][]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON response: %w", err)
	}

	// Extract the list of repositories
	repos, ok := result["repositories"]
	if !ok {
		return nil, fmt.Errorf("repositories not found in response")
	}

	return repos, nil
}

func ListRepos(ctx context.Context) []string {
	// Encode credentials for Basic Authentication
	_ = authn.AuthConfig{
		Username: "admin",
		Password: "Harbor12345",
	}

	// Encode credentials for Basic Authentication
	username := "admin"
	password := "Harbor12345"
	auths := base64.URLEncoding.EncodeToString([]byte(username + ":" + password))
	fmt.Println(auths)
	auths = "Basic " + auths
	option := crane.WithAuth(&authn.Basic{Username: auths, Password: "Harbor12345"})
	fmt.Println(auths)

	baseURL := "demo.goharbor.io"

	// option := crane.WithAuth(&authn.Bearer{Token: auths})

	catalog, err := crane.Catalog(baseURL, option, crane.Insecure)
	if err != nil {
		log.Printf("error in catalog crane: %v", err)
	}

	return catalog

	// fmt.Println("Catalog: ", catalog)
}
