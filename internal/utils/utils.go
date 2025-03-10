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

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
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
	_, err := url.Parse(config.GetRemoteRegistryURL())
	if err != nil {
		return fmt.Errorf("error parsing URL: %w", err)
	}
	return config.SetRemoteRegistryURL(FormatRegistryURL(config.GetRemoteRegistryURL()))
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

func SetupContext(context context.Context) context.Context {
	ctx, _ := signal.NotifyContext(context, syscall.SIGTERM, syscall.SIGINT)
	return ctx
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

func HandleErrorAndWarning(log *zerolog.Logger, errors []error, warnings []config.Warning) error {
	for i := range warnings {
		log.Warn().Msg(string(warnings[i]))
	}
	for i := range errors {
		log.Error().Msg(errors[i].Error())
	}
	if len(errors) > 0 {
		return fmt.Errorf("error initializing config")
	}
	return nil
}

func Init(ctx context.Context) (context.Context, *errgroup.Group, scheduler.Scheduler, error) {
	wg, ctx := errgroup.WithContext(ctx)
	errors, warnings := config.InitConfig(config.DefaultConfigPath)
	log := logger.NewLogger(config.GetLogLevel())
	if err := HandleErrorAndWarning(log, errors, warnings); err != nil {
		return nil, nil, nil, err
	}
	ctx = context.WithValue(ctx, logger.LoggerKey, log)

	log.Debug().Msg("Initializing new basic scheduler for cron jobs")
	scheduler := scheduler.NewBasicScheduler(ctx, log)

	ctx = context.WithValue(ctx, scheduler.GetSchedulerKey(), scheduler)

	return ctx, wg, scheduler, nil
}

func IsZTRDone() bool {
	return config.GetSourceRegistryURL() != ""
}
