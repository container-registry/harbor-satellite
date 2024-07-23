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
	bearerToken := fmt.Sprintf("Bearer %s", os.Getenv("TOKEN"))

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Printf("error in creating request: %v", err)
	}
	req.Header.Add("Authorization", bearerToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)

	// snippet only
	var result []Image
	if err := json.Unmarshal(body, &result); err != nil { // Parse []byte to go struct pointer
		fmt.Println("Can not unmarshal JSON")
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
