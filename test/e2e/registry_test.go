package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dagger.io/dagger"
	"github.com/stretchr/testify/assert"
)

const (
	regUrl        = "localhost:5000"
	imageToPush   = "ubuntu" // Image to push to the registry
	imageVersion  = "golang:1.22"
	exposePort    = 9090
	containerPort = 9090
	appDir        = "/app"
	appBinary     = "app"
	sourceFile    = "main.go"
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
	remote, err := setupRemoteRegistry(client, ctx)
	assert.NoError(t, err, "Failed to set up remote registry")
	// Set up the container registry
	registry, err := setupContainerRegistry(client, ctx)
	assert.NoError(t, err, "Failed to set up container registry")
	// reg, _ := registry.Hostname(ctx)
	// fmt.Println(reg)

	// Push the image to the registry
	pushImageToRegistry(ctx, client, remote)
	assert.NoError(t, err, "Failed to upload image to registry")

	// Implement the Satellite Testing
	buildSatellite(client, ctx, registry, remote)
	assert.NoError(t, err, "Failed to build Satellite")
}

// Setup Container Registry as a Dagger Service
func setupRemoteRegistry(
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
		AsService().Start(ctx)
	if err != nil {
		return nil, err
	}

	// Return the registry URL
	return container, nil
}

// Setup Container Registry as a Dagger Service
func setupContainerRegistry(
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
		AsService().Start(ctx)
	if err != nil {
		log.Fatal("Error while creating registry: ", err)
	}

	// Return the registry URL
	return container, nil
}

// Upload image to the registry
func pushImageToRegistry(
	ctx context.Context,
	client *dagger.Client,
	registry *dagger.Service,
) {
	// // socket to connect to host Docker
	socket := client.Host().UnixSocket("/var/run/docker.sock")
	// newUrl := strings.Replace(srvAddr, "tcp://", "", 1)
	// fmt.Println(newUrl)

	container := client.Container().
		From("alpine").
    WithUnixSocket("/var/run/docker.sock", socket).
		WithEnvVariable("DOCKER_HOST", "unix:///var/run/docker.sock").
		WithServiceBinding("remote", registry)

	log.Println("completed setting up the pushing container")

	log.Println("going to pull and push the image")

	// add crane push images
	container = container.WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "copy", "busybox:stable", "remote:5000/library/busybox:stable", "--insecure"}).
		WithExec([]string{"crane", "copy", "busybox:latest", "remote:5000/library/busybox:latest", "--insecure"})

	container = container.WithExec([]string{"crane", "catalog", "remote:5000", "--insecure"})

	prints, _ := container.Stdout(ctx)
	fmt.Println(prints)
}

// buildSatellite and test test the connection
func buildSatellite(
	client *dagger.Client,
	ctx context.Context,
	registry *dagger.Service,
	remote *dagger.Service,
) *dagger.Service {
	// slog.Info("Starting the Satellite build process...")
	// endp, err := registry.Endpoint(ctx)
	// hostname, err := registry.Hostname(ctx)
	// ports, err := registry.Ports(ctx)
	// fmt.Println(
	// 	"\n\n\n \n \n ------------------------------------------------------------------------------",
	// )
	// fmt.Printf("endpoint: %s", endp)
	// fmt.Printf("hostname: %s", hostname)
	// fmt.Printf("ports: %v %v", ports, srvAddr)

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
		WithServiceBinding("reg", registry).
		WithServiceBinding("remote", remote).
    WithUnixSocket("/var/run/docker.sock", socket).
		WithEnvVariable("DOCKER_HOST", "unix:///var/run/docker.sock").
		WithExec([]string{"cat", "config.toml"}).
		WithFile("./config.toml", configFile).
		WithExec([]string{"cat", "config.toml"}).
		WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "-v", "catalog", "reg:5000", "--insecure"}).
		WithExec([]string{"crane", "-v", "catalog", "remote:5000", "--insecure"}).
		WithExec([]string{"go", "build", "-o", appBinary, sourceFile}).
		WithExposedPort(containerPort).
		WithExec([]string{"./" + appBinary})

	service, err := container.AsService().Start(ctx)
	if err != nil {
		log.Fatalf("Error in running Satellite: %v", err)
	}

	slog.Info("Satellite service is running and accessible")

	prints, _ := container.Stdout(ctx)
	fmt.Println(prints)

	return service
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
