package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

type ZotConfig struct {
	DistSpecVersion string           `json:"distSpecVersion"`
	Storage         ZotStorageConfig `json:"storage"`
	HTTP            ZotHTTPConfig    `json:"http"`
	Log             ZotLogConfig     `json:"log"`
	RemoteURL       string           `json:"remoteURL"`
	Extensions      ZotExtensions    `json:"extensions"`
}

type ZotExtensions struct {
	// Sync should be specified before Scrub
	Scrub ScrubConfig `json:"scrub"`
}

type ScrubConfig struct {
	Enable bool `json:"enable"`
	// needs to be validated to be a valid time interval
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

func (c *ZotConfig) GetLocalRegistryURL() string {
	address := c.HTTP.Address
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return fmt.Sprintf("%s:%s", address, c.HTTP.Port)
}

// ReadConfig reads a JSON file from the specified path and unmarshals it into a Config struct.
func ReadAndValidateZotConfig(filePath string, zotConfig *ZotConfig) error {
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
	return zotConfig.validate()
}

func (c *ZotConfig) validate() error {
	if c.DistSpecVersion == "" {
		return errors.New("DistSpecVersion cannot be empty")
	}

	if c.Storage.RootDirectory == "" {
		return errors.New("storage.rootDirectory cannot be empty")
	}

	if err := validateHTTPConfig(c.HTTP); err != nil {
		return err
	}

	if err := validateLogConfig(c.Log); err != nil {
		return err
	}

	if c.Extensions.Scrub.Enable {
		if err := validateInterval(c.Extensions.Scrub.Interval); err != nil {
			return err
		}
	}

	return nil
}

func validateHTTPConfig(http ZotHTTPConfig) error {
	if http.Address == "" {
		return errors.New("http.address cannot be empty")
	}

	portPattern := `^\d{1,5}$`
	if matched, _ := regexp.MatchString(portPattern, http.Port); !matched {
		return errors.New("http.port must be a valid numeric string")
	}
	return nil
}

func validateLogConfig(log ZotLogConfig) error {
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[log.Level] {
		return errors.New("log.level must be one of: debug, info, warn, error")
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
