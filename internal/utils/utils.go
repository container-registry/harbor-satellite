package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/images"
	"container-registry.com/harbor-satellite/registry"
)

// / ValidateRegistryAddress validates the registry address and port and returns the URL
func ValidateRegistryAddress(registryAdr, registryPort string) (string, error) {
	ip := net.ParseIP(registryAdr)
	if ip == nil {
		return "", errors.New("invalid IP address")
	}
	if ip.To4() != nil {
	} else {
		return "", errors.New("IP address is IPv6 format and unsupported")
	}

	return fmt.Sprintf("%s:%s", registryAdr, registryPort), nil
}

// / HandleOwnRegistry handles the own registry address and port and sets the Zot URL
func HandleOwnRegistry() error {
	zotURL, err := ValidateRegistryAddress(config.GetOwnRegistryAdr(), config.GetOwnRegistryPort())
	if err != nil {
		return err
	}
	config.SetZotURL(zotURL)
	return nil
}

// LaunchDefaultZotRegistry launches the default Zot registry using the Zot config path
func LaunchDefaultZotRegistry() error {
	launch, err := registry.LaunchRegistry(config.GetZotConfigPath())
	if !launch {
		return fmt.Errorf("error launching registry: %w", err)
	}
	if err != nil {
		return fmt.Errorf("error launching registry: %w", err)
	}
	return nil
}

// Helper function to determine if input is a valid URL
func IsValidURL(input string) bool {
	parsedURL, err := url.Parse(input)
	return err == nil && parsedURL.Scheme != ""
}

// GetAbsFilePath gets the absolute file path of the input file path and checks if it exists
func GetAbsFilePath(input string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	absPath := filepath.Join(dir, input)
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return err
	}
	return nil
}

// Check if path contains invalid characters
func HasInvalidPathChars(input string) bool {
	return strings.ContainsAny(input, "\\:*?\"<>|")
}

// ParseImagesJsonFile parses the images.json file and decodes it into the ImageList struct
func ParseImagesJsonFile(absPath string, imagesList *images.ImageList) error {
	file, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(imagesList); err != nil {
		return err
	}
	return nil
}

// Set registry environment variables
func SetRegistryEnvVars(imageList images.ImageList) error {
	registryURL := imageList.RegistryURL
	registryParts := strings.Split(registryURL, "/")
	if len(registryParts) < 3 {
		return fmt.Errorf("invalid registryUrl format in images.json")
	}

	os.Setenv("REGISTRY", registryParts[2])
	config.SetRegistry(registryParts[2])

	if len(imageList.Repositories) > 0 {
		os.Setenv("REPOSITORY", imageList.Repositories[0].Repository)
		config.SetRepository(imageList.Repositories[0].Repository)
	} else {
		return fmt.Errorf("no repositories found in images.json")
	}

	return nil
}

// SetUrlConfig sets the URL configuration for the input URL and sets the environment variables
func SetUrlConfig(input string) {
	os.Setenv("USER_INPUT", input)
	config.SetUserInput(input)
	parts := strings.SplitN(input, "://", 2)
	scheme := parts[0] + "://"
	os.Setenv("SCHEME", scheme)
	config.SetScheme(scheme)
	registryAndPath := parts[1]
	registryParts := strings.Split(registryAndPath, "/")
	os.Setenv("REGISTRY", registryParts[0])
	config.SetRegistry(registryParts[0])
	os.Setenv("API_VERSION", registryParts[1])
	config.SetAPIVersion(registryParts[1])
	os.Setenv("REPOSITORY", registryParts[2])
	config.SetRepository(registryParts[2])
	os.Setenv("IMAGE", registryParts[3])
	config.SetImage(registryParts[3])
}
