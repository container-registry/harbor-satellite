package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// Predefined correct extension order.
var validExtensionsOrder = []string{
	"sync", "search", "scrub", "metrics", "lint", "ui", "mgmt", "userprefs", "imagetrust",
}

type ZotConfig struct {
	DistSpecVersion string           `json:"distSpecVersion"`
	Storage         ZotStorageConfig `json:"storage"`
	HTTP            ZotHTTPConfig    `json:"http"`
	Log             ZotLogConfig     `json:"log"`
	RemoteURL       string           `json:"remoteURL"`
	Extensions      *ZotExtensions   `json:"extensions,omitempty"`
}

type ZotExtensions struct {
	Sync  SyncConfig  `json:"sync"`
	Scrub ScrubConfig `json:"scrub"`
}

type SyncConfig struct {
	Enable          bool             `json:"enable"`
	CredentialsFile string           `json:"credentialsFile,omitempty"`
	Registries      []RegistryConfig `json:"registries"`
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

// Though Zot serve command will validate and throw an error, we can validate earlier and let the user know
// without failing the zot setup.
func (c *ZotConfig) Validate() error {
	if c.DistSpecVersion == "" {
		return fmt.Errorf("DistSpecVersion cannot be empty")
	}

	if c.Storage.RootDirectory == "" {
		return fmt.Errorf("storage.rootDirectory cannot be empty")
	}

	if err := validateHTTPConfig(c.HTTP); err != nil {
		return err
	}

	if err := validateLogConfig(c.Log); err != nil {
		return err
	}

	if c.Extensions != nil {
		if c.Extensions.Scrub.Enable {
			if err := validateInterval(c.Extensions.Scrub.Interval); err != nil {
				return err
			}
		}

		if c.Extensions.Sync.Enable {
			if err := validateSyncConfig(c.Extensions.Sync); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateSyncConfig(sync SyncConfig) error {
	if len(sync.Registries) == 0 {
		return fmt.Errorf("sync.registries must not be empty if sync is enabled")
	}

	for i, reg := range sync.Registries {
		if len(reg.URLs) == 0 {
			return fmt.Errorf("sync.registries[%d].urls must not be empty", i)
		}

		for j, registryUrl := range reg.URLs {
			if _, err := url.Parse(registryUrl); err != nil {
				return fmt.Errorf("sync.registries[%d].urls[%d] is not a valid URL: %w", i, j, err)
			}
		}

		if reg.PollInterval != "" {
			if err := validateInterval(reg.PollInterval); err != nil {
				return fmt.Errorf("sync.registries[%d].pollInterval is invalid: %w", i, err)
			}
		}

		if reg.MaxRetries < 0 {
			return fmt.Errorf("sync.registries[%d].maxRetries cannot be negative", i)
		}

		if reg.RetryDelay != "" {
			if err := validateInterval(reg.RetryDelay); err != nil {
				return fmt.Errorf("sync.registries[%d].retryDelay is invalid: %w", i, err)
			}
		}

		for k, content := range reg.Content {
			if content.Prefix == "" {
				return fmt.Errorf("sync.registries[%d].content[%d].prefix must not be empty", i, k)
			}
			if content.Destination == "" {
				return fmt.Errorf("sync.registries[%d].content[%d].destination must not be empty", i, k)
			}
			if content.Tags != nil {
				if err := validateTagsConfig(content.Tags); err != nil {
					return fmt.Errorf("sync.registries[%d].content[%d].tags is invalid: %w", i, k, err)
				}
			}
		}
	}

	return nil
}

func validateTagsConfig(tags *TagsConfig) error {
	if tags.Regex != "" {
		if _, err := regexp.Compile(tags.Regex); err != nil {
			return fmt.Errorf("tags.regex is not a valid regex: %w", err)
		}
	}
	return nil
}
func validateHTTPConfig(http ZotHTTPConfig) error {
	if http.Address == "" {
		return fmt.Errorf("http.address cannot be empty")
	}

	portPattern := `^\d{1,5}$`
	if matched, _ := regexp.MatchString(portPattern, http.Port); !matched {
		return fmt.Errorf("http.port must be a valid numeric string")
	}
	return nil
}

func validateLogConfig(log ZotLogConfig) error {
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[log.Level] {
		return fmt.Errorf("log.level must be one of: debug, info, warn, error")
	}
	return nil
}

func validateInterval(interval string) error {
	_, err := time.ParseDuration(interval)
	if err != nil {
		return fmt.Errorf("invalid interval format: %w", err)
	}
	return nil
}

func (c *ZotConfig) SetZotRemoteURL(url string) {
	c.RemoteURL = url
}

// Helper function to find the index of a string in a slice
func indexOf(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}
