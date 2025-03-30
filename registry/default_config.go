package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

type DefaultZotConfig struct {
	DistSpecVersion string `json:"distSpecVersion"`
	Storage         struct {
		RootDirectory string `json:"rootDirectory"`
	} `json:"storage"`
	HTTP struct {
		Address string `json:"address"`
		Port    string `json:"port"`
	} `json:"http"`
	Log struct {
		Level string `json:"level"`
	} `json:"log"`
	RemoteURL string
}

func (c *DefaultZotConfig) GetLocalRegistryURL() string {
	address := c.HTTP.Address
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return fmt.Sprintf("%s:%s", address, c.HTTP.Port)
}

// ReadConfig reads a JSON file from the specified path and unmarshals it into a Config struct.
func ReadConfig(filePath string, zotConfig *DefaultZotConfig) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("error closing file: %v", err)
		}
	}()

	// Read the file contents
	bytes, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("could not read file: %w", err)
	}

	// Unmarshal the JSON into a Config struct
	err = json.Unmarshal(bytes, &zotConfig)
	if err != nil {
		return fmt.Errorf("could not unmarshal JSON: %w", err)
	}
	return nil
}

func (c *DefaultZotConfig) SetZotRemoteURL(url string) {
	c.RemoteURL = url
}
