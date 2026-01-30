package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

type ZotConfig struct {
	HTTP ZotHTTPConfig `json:"http"`
	Log  ZotLogConfig  `json:"log"`
}

type ZotHTTPConfig struct {
	Address string `json:"address"`
	Port    string `json:"port"`
}

type ZotLogConfig struct {
	Level string `json:"level"`
}

func (c *ZotConfig) GetRegistryURL() string {
	address := c.HTTP.Address
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return fmt.Sprintf("%s:%s", address, c.HTTP.Port)
}

// ReadConfig reads a JSON file from the specified path and unmarshals it into a ZotConfig struct.
func ReadZotConfig(filePath string, zotConfig *ZotConfig) error {
	file, err := os.Open(filePath) //nolint:gosec // G304: path from internal config
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
