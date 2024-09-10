package reg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

type AppError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}
type TagListResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func FetchRepos(username, password, url string) ([]string, error) {
	// Sample Data
	// username := "admin"
	// password := "Harbor12345"
	// url := "https://demo.goharbor.io"

	url = url + "/v2/_catalog?n=1000"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Encode credentials for Basic Authentication
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	req.Header.Set("Authorization", "Basic "+auth)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

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

