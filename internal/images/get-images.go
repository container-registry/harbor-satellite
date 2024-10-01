package images

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type ImageList struct {
	RegistryURL  string `json:"registryUrl"`
	Repositories []struct {
		Repository string `json:"repository"`
		Images     []struct {
			Name string `json:"name"`
		} `json:"images"`
	} `json:"repositories"`
}

type Image struct {
	ID         int       `json:"ID"`
	Registry   string    `json:"Registry"`
	Repository string    `json:"Repository"`
	Tag        string    `json:"Tag"`
	Digest     string    `json:"Digest"`
	CreatedAt  time.Time `json:"CreatedAt"`
	UpdatedAt  time.Time `json:"UpdatedAt"`
}

func GetImages(url string) (string, error) {
	token := os.Getenv("TOKEN")
	if token == "" {
		log.Fatal("Token cannot be empty")
	}

	bearerToken := fmt.Sprintf("Bearer %s", token)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Printf("error in creating request: %v", err)
		return "", err
	}
	req.Header.Add("Authorization", bearerToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println(err)
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	// snippet only
	var result []Image
	if err := json.Unmarshal(body, &result); err != nil { // Parse []byte to go struct pointer
		log.Println("Can not unmarshal JSON")
		return "", err
	}

	fmt.Println(result)

	for _, img := range result {
		url := fmt.Sprintf("http://%s/%s", img.Registry, img.Repository)
		fmt.Println("url: ", url)
		return url, nil
	}

	return "", nil
}
