package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
}

func (c *DefaultZotConfig) GetLocalRegistryURL() string {
	return fmt.Sprintf("%s:%s", c.HTTP.Address, c.HTTP.Port)
}

// ReadConfig reads a JSON file from the specified path and unmarshals it into a Config struct.
func ReadConfig(filePath string) (*DefaultZotConfig, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()

	// Read the file contents
	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("could not read file: %w", err)
	}

	// Unmarshal the JSON into a Config struct
	var config DefaultZotConfig
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal JSON: %w", err)
	}

	return &config, nil
}
