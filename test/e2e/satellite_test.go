package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dagger.io/dagger"
	"github.com/stretchr/testify/assert"
)

const (
	appDir     = "/app"
	appBinary  = "app"
	sourceFile = "main.go"
)

func TestSetupContainerRegistry(t *testing.T) {
	ctx := context.Background()

	// Initialize Dagger client
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		log.Fatalf("Failed to connect to Dagger: %v", err)
	}
	defer client.Close()

	// Set up remote Registry
	remote, err := setupRemoteRegistry(t, client, ctx)
	assert.NoError(t, err, "Failed to set up remote registry")
	// Set up the container registry
	registry, err := setupContainerRegistry(t, client, ctx)
	assert.NoError(t, err, "Failed to set up container registry")
	// reg, _ := registry.Hostname(ctx)
	// fmt.Println(reg)

	// Push the image to the registry
	pushImageToRegistry(t, ctx, client, remote)
	assert.NoError(t, err, "Failed to upload image to registry")

	// Implement the Satellite Testing
	stdOut := buildSatellite(t, client, ctx, remote, registry)
	assert.NoError(t, err, "Failed to build Satellite")
	fmt.Println(stdOut)
}

// Setup Container Registry as a Dagger Service
func setupRemoteRegistry(
	t *testing.T,
	client *dagger.Client,
	ctx context.Context,
) (*dagger.Service, error) {
	// socket to connect to host Docker
	socket := client.Host().UnixSocket("/var/run/docker.sock")

	// Pull the Harbor registry image
	container, err := client.Container().
		From("registry:2").
		WithExposedPort(5000).
		WithUnixSocket("/var/run/docker.sock", socket).
		WithEnvVariable("DOCKER_HOST", "unix:///var/run/docker.sock").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		AsService().Start(ctx)

	assert.NoError(t, err, "Failed in setting up remote registry.")

	// Return the registry URL
	return container, nil
}

// Setup Container Registry as a Dagger Service
func setupContainerRegistry(
	t *testing.T,
	client *dagger.Client,
	ctx context.Context,
) (*dagger.Service, error) {
	// socket to connect to host Docker
	socket := client.Host().UnixSocket("/var/run/docker.sock")

	// Pull the registry image
	container, err := client.Container().
		From("registry:2").
		WithExposedPort(5000, dagger.ContainerWithExposedPortOpts{Protocol: "TCP"}).
		WithUnixSocket("/var/run/docker.sock", socket).
		WithEnvVariable("DOCKER_HOST", "unix:///var/run/docker.sock").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		AsService().Start(ctx)

	assert.NoError(t, err, "Failed in setting up registry")

	// Return the registry URL
	return container, nil
}

// Upload image to the registry
func pushImageToRegistry(
	t *testing.T,
	ctx context.Context,
	client *dagger.Client,
	registry *dagger.Service,
) {
	// // socket to connect to host Docker
	socket := client.Host().UnixSocket("/var/run/docker.sock")
	// newUrl := strings.Replace(srvAddr, "tcp://", "", 1)
	// fmt.Println(newUrl)

	container := client.Container().
		From("docker:dind").
		WithUnixSocket("/var/run/docker.sock", socket).
		WithEnvVariable("DOCKER_HOST", "unix:///var/run/docker.sock").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithServiceBinding("remote", registry)

	// add crane push images
	container = container.WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"docker", "pull", "busybox:1.36"}).
		WithExec([]string{"docker", "pull", "busybox:stable"}).
		WithExec([]string{"crane", "copy", "busybox:1.36", "remote:5000/library/busybox:1.36", "--insecure"}).
		WithExec([]string{"crane", "copy", "busybox:stable", "remote:5000/library/busybox:stable", "--insecure"}).
		WithExec([]string{"crane", "digest", "remote:5000/library/busybox:1.36", "--insecure"}).
		WithExec([]string{"crane", "digest", "remote:5000/library/busybox:stable", "--insecure"})

	container = container.WithExec([]string{"crane", "catalog", "remote:5000", "--insecure"})

	prints, err := container.Stdout(ctx)
	assert.NoError(t, err, "Failed to push image to remote registry")
	fmt.Println(prints)
}

// buildSatellite and test test the connection
func buildSatellite(
	t *testing.T,
	client *dagger.Client,
	ctx context.Context,
	remote *dagger.Service,
	registry *dagger.Service,
) *dagger.Container {
	// Get the directory of project located one level up from the current working directory
	parentDir, err := getProjectDir()
	if err != nil {
		log.Fatalf("Error getting parentDirectory: %v", err)
	}

	// Use the parent directory path in Dagger
	dir := client.Host().Directory(parentDir)

	// Create the configuration file on the host
	configFile := client.Host().File("./testdata/config.toml")

	// File path to write the config.toml
	// filePath := "./testdata/config.toml"

	// Generate the config file
	// err = generateConfigFile(filePath, srvAddr)
	// if err != nil {
	// 	log.Fatalf("Failed to generate config file: %v", err)
	// }

	// Pull the image from Docker Hub
	socket := client.Host().UnixSocket("/var/run/docker.sock")

	// Configure and build the container

	container := client.Container().From("golang:alpine").WithDirectory(appDir, dir).
		WithWorkdir(appDir).
		WithServiceBinding("remote", remote).
		WithServiceBinding("reg", registry).
		WithUnixSocket("/var/run/docker.sock", socket).
		WithEnvVariable("DOCKER_HOST", "unix:///var/run/docker.sock").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"cat", "config.toml"}).
		WithFile("./config.toml", configFile).
		WithExec([]string{"cat", "config.toml"}).
		WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "-v", "catalog", "reg:5000", "--insecure"}).
		WithExec([]string{"crane", "-v", "catalog", "remote:5000", "--insecure"}).
		WithExec([]string{"crane", "digest", "remote:5000/library/busybox:stable", "--insecure"}).
		WithExec([]string{"go", "build", "-o", appBinary, sourceFile}).
		WithExposedPort(9090).
		WithExec([]string{"go", "run", "./test/e2e/test.go"})

	assert.NoError(t, err, "Test failed in buildSatellite")
	// service, err := container.AsService().Start(ctx)
	// if err != nil {
	// 	log.Fatalf("Error in running Satellite: %v", err)
	// }

	slog.Info("Satellite is running and accessible")

	prints, err := container.Stdout(ctx)
	fmt.Println(prints)

	return container
}

// getProjectDir gets the directory of the project
func getProjectDir() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(currentDir, "../.."))
}

func generateConfigFile(filePath string, srvAddr string) error {
	// Define the TOML content
	configContent := `
# Auto-generated
bring_own_registry = true
url_or_file = "https://demo.goharbor.io/v2/library/registry"
`
	// addr := strings.TrimPrefix(srvAddr, "http://")

	configContent = configContent + fmt.Sprintf("own_registry_adr = \"%s\"", "reg:5000")

	// Create or open the file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write the TOML content to the file
	_, err = file.WriteString(configContent)
	if err != nil {
		return err
	}

	return nil
}
