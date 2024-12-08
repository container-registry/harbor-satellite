package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dagger.io/dagger"
	"github.com/stretchr/testify/assert"
)

func TestSatellite(t *testing.T) {
	ctx := context.Background()

	// Initialize Dagger client
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	assert.NoError(t, err, "Failed to connect to Dagger")
	defer client.Close()

	// Set up Source Registry
	source, err := setupSourceRegistry(t, client, ctx)
	assert.NoError(t, err, "Failed to set up source registry")

	// Set up Destination registry
	dest, err := setupDestinationRegistry(t, client, ctx)
	assert.NoError(t, err, "Failed to set up destination registry")

	// Push images to Source registry
	pushImageToSourceRegistry(t, ctx, client, source)
	assert.NoError(t, err, "Failed to upload image to source registry")

	// Build & Run Satellite
	buildSatellite(t, client, ctx, source, dest)
}

// Setup Source Registry as a Dagger Service
func setupSourceRegistry(
	t *testing.T,
	client *dagger.Client,
	ctx context.Context,
) (*dagger.Service, error) {

	container, err := client.Container().
		From("registry:2").
		WithExposedPort(5000).
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		AsService().Start(ctx)

	assert.NoError(t, err, "Failed setting up source registry.")

	return container, nil
}

// Setup Destination Registry as a Dagger Service
func setupDestinationRegistry(
	t *testing.T,
	client *dagger.Client,
	ctx context.Context,
) (*dagger.Service, error) {

	container, err := client.Container().
		From("registry:2").
		WithExposedPort(5000).
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		AsService().Start(ctx)

	assert.NoError(t, err, "Failed setting up destination registry")

	return container, nil
}

// Push image to the Source registry
func pushImageToSourceRegistry(
	t *testing.T,
	ctx context.Context,
	client *dagger.Client,
	source *dagger.Service,
) {

	container := client.Container().
		From("docker:dind").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithServiceBinding("source", source)

	// add crane & push images
	container = container.WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "copy", "busybox:1.36", "source:5000/library/busybox:1.36", "--insecure"}).
		WithExec([]string{"crane", "digest", "source:5000/library/busybox:1.36", "--insecure"})

	// check pushed images exist
	container = container.WithExec([]string{"crane", "catalog", "source:5000", "--insecure"})

	stdOut, err := container.Stdout(ctx)
	assert.NoError(t, err, "Failed to print stdOut in pushing Image to Source")

	fmt.Println(stdOut)
}

func buildSatellite(
	t *testing.T,
	client *dagger.Client,
	ctx context.Context,
	source *dagger.Service,
	dest *dagger.Service,
) {
	var PATH_TO_CONFIG string
	if ABS {
		PATH_TO_CONFIG = absolute_path
	} else {
		PATH_TO_CONFIG = relative_path
	}
	// Get the directory
	parentDir, err := getProjectDir()
	assert.NoError(t, err, "Failed to get Project Directory")

	// Use the directory path in Dagger
	dir := client.Host().Directory(parentDir)

	// Get configuration file on the host
	configFile := client.Host().File(PATH_TO_CONFIG)

	// Configure and build the Satellite
	container := client.Container().From("golang:alpine").WithDirectory(appDir, dir).
		WithWorkdir(appDir).
		WithServiceBinding("source", source).
		WithServiceBinding("dest", dest).
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithFile("./config.json", configFile).
		WithExec([]string{"go", "build", "-o", appBinary, sourceFile}).
		WithExposedPort(9090).
		WithExec([]string{"./" + appBinary}).
		AsService()

	satellite_container := client.Container().
		From("golang:alpine").
		WithServiceBinding("satellite", container).
		WithExec([]string{"wget", "-O", "-", "http://satellite:9090" + satellite_ping_endpoint})

	output, err := satellite_container.Stdout(ctx)
	assert.NoError(t, err, "Failed to get output from satellite ping endpoint")
	// Check the response
	var response map[string]interface{}
	err = json.Unmarshal([]byte(output), &response)
	assert.NoError(t, err, "Failed to parse JSON response")

	// Assert the response
	assert.Equal(t, true, response["success"], "Unexpected success value")
	assert.Equal(t, "Ping satellite successful", response["message"], "Unexpected message")
	assert.Equal(t, float64(200), response["status_code"], "Unexpected status code")
}

// Gets the directory of the project
func getProjectDir() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(currentDir, "../.."))
}
