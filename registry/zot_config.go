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

type RegistryConfig struct {
	URLs         []string        `json:"urls"`
	OnDemand     bool            `json:"onDemand"`
	PollInterval string          `json:"pollInterval"`
	TLSVerify    bool            `json:"tlsVerify"`
	CertDir      string          `json:"certDir"`
	MaxRetries   int             `json:"maxRetries"`
	RetryDelay   string          `json:"retryDelay"`
	OnlySigned   bool            `json:"onlySigned"`
	Content      []ContentConfig `json:"content"`
}

type ContentConfig struct {
	Prefix      string      `json:"prefix"`
	Destination string      `json:"destination"`
	StripPrefix bool        `json:"stripPrefix"`
	Tags        *TagsConfig `json:"tags"`
}

type TagsConfig struct {
	Regex  string `json:"regex"`
	Semver bool   `json:"semver"`
}

type ScrubConfig struct {
	Enable   bool   `json:"enable"`
	Interval string `json:"interval"`
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

func (c *ZotConfig) GetRegistryURL() string {
	address := c.HTTP.Address
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return fmt.Sprintf("%s:%s", address, c.HTTP.Port)
}

// ReadConfig reads a JSON file from the specified path and unmarshals it into a ZotConfig struct.
func ReadZotConfig(filePath string, zotConfig *ZotConfig) error {
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
