package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type ZotConfig struct {
	DistSpecVersion string           `json:"distSpecVersion"`
	Storage         ZotStorageConfig `json:"storage"`
	HTTP            ZotHTTPConfig    `json:"http"`
	Log             ZotLogConfig     `json:"log"`
	RemoteURL       string           `json:"remoteURL"`
}

type ZotStorageConfig struct {
	RootDirectory string `json:"rootDirectory"`
}

type ZotHTTPConfig struct {
	Address string `json:"address"`
	Port    string `json:"port"`
}

type ZotLogConfig struct {
	Level string `json:"level"`
}

func (c *DefaultZotConfig) GetLocalRegistryURL() string {
	address := c.HTTP.Address
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return fmt.Sprintf("%s:%s", address, c.HTTP.Port)
}

// ReadConfig reads a JSON file from the specified path and unmarshals it into a Config struct.
func ReadConfig(filePath string, zotConfig *ZotConfig) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()

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

func (c *ZotConfig) SetZotRemoteURL(url string) {
	c.RemoteURL = url
}
