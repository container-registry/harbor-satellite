package utils

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/pkg/config"
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
func HandleOwnRegistry(cm *config.ConfigManager) error {
	remoteRegistryURL := string(cm.GetRemoteRegistryURL())
	_, err := url.Parse(remoteRegistryURL)
	if err != nil {
		return fmt.Errorf("error parsing URL: %w", err)
	}
	cm = cm.With(config.SetLocalRegistryURL(FormatRegistryURL(remoteRegistryURL)))
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

func SetupContext(context context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context, syscall.SIGTERM, syscall.SIGINT)
	return ctx, cancel
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
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("error closing file: %v", err)
		}
	}()

	_, err = file.Write(data)
	if err != nil {
		return fmt.Errorf("error while write to the file :%s", err)
	}
	return nil
}

func HandleWarnings(log *zerolog.Logger, warnings []string) {
	for i := range warnings {
		log.Warn().Msg(string(warnings[i]))
	}
}

func IsZTRDone() bool {
	return config.GetSourceRegistryURL() != ""
}

func InitConfig(path string) (*config.ConfigManager, []string, error) {
	cfg, err := config.ReadConfig(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read config: %w", err)
	}

	warnings := config.ValidateConfig(cfg)

	cm, err := config.NewConfigManager(path, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	return cm, warnings, nil
}

func InitLogger(ctx context.Context, warnings []string) (context.Context, *zerolog.Logger) {
	log := logger.NewLogger(config.GetLogLevel())
	HandleWarnings(log, warnings)
	ctx = context.WithValue(ctx, logger.LoggerKey, log)
	return ctx, log
}

func InitScheduler(ctx context.Context) (context.Context, scheduler.Scheduler) {
	log := logger.FromContext(ctx)
	log.Debug().Msg("Initializing new basic scheduler for cron jobs")

	s := scheduler.NewBasicScheduler(ctx, log)
	ctx = context.WithValue(ctx, s.GetSchedulerKey(), s)

	return ctx, s
}

func InitAppContext(ctx context.Context, warnings []string) (context.Context, *errgroup.Group, scheduler.Scheduler, error) {
	wg, ctx := errgroup.WithContext(ctx)

	ctx, err := InitLogger(ctx, warnings)
	if err != nil {
		return nil, nil, nil, err
	}

	ctx, s := InitScheduler(ctx)

	return ctx, wg, s, nil
}
