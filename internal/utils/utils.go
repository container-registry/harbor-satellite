package utils

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/logger"
	"container-registry.com/harbor-satellite/registry"
	"github.com/spf13/cobra"
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
	port, err := strconv.Atoi(registryPort)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("invalid port number")
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
	defaultZotURL := fmt.Sprintf("%s:%s", "127.0.0.1", "8585")
	config.SetZotURL(defaultZotURL)
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

func GetRepositoryAndImageNameFromArtifact(repository string) (string, string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repository format: %s. Expected format: repo/image", repository)
	}
	repo := parts[0]
	image := parts[1]
	return repo, image, nil
}

func FormatDuration(input string) (string, error) {
	seconds, err := strconv.Atoi(input) // Convert input string to an integer
	if err != nil {
		return "", errors.New("invalid input: not a valid number")
	}
	if seconds < 0 {
		return "", errors.New("invalid input: seconds cannot be negative")
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secondsRemaining := seconds % 60

	var result string

	if hours > 0 {
		result += strconv.Itoa(hours) + "h"
	}
	if minutes > 0 {
		result += strconv.Itoa(minutes) + "m"
	}
	if secondsRemaining > 0 || result == "" {
		result += strconv.Itoa(secondsRemaining) + "s"
	}

	return result, nil
}

func SetupContext(context context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context, syscall.SIGTERM, syscall.SIGINT)
	return ctx, cancel
}

func SetupContextForCommand(cmd *cobra.Command) {
	ctx := cmd.Context()
	ctx = logger.AddLoggerToContext(ctx, config.GetLogLevel())
	cmd.SetContext(ctx)
}

// FormatRegistryURL formats the registry URL by trimming the "https://" or "http://" prefix if present
func FormatRegistryURL(url string) string {
	// Trim the "https://" or "http://" prefix if present
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	return url
}

func ReadFile(path string, shouldPrint bool) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if shouldPrint {
		PrintData(string(data))
	}
	return data, nil
}

// PrintData prints the content of a file line by line
func PrintData(content string) {
	lines := strings.Split(content, "\n")
	fmt.Print("\n")
	for i, line := range lines {
		fmt.Printf("%5d | %s\n", i+1, line)
	}
}

// WriteFile takes the path and the data wand write the data to the file
func WriteFile(path string, data []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating file :%s", err)
	}
	defer file.Close()
	_, err = file.Write(data)
	if err != nil {
		return fmt.Errorf("error while write to the file :%s", err)
	}
	return nil
}
