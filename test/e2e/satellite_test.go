package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// Build & Run Satellite
	buildSatellite(t, client, ctx)
}

// Setup Source Registry as a Dagger Service
func setupSourceRegistry(
	client *dagger.Client,
	ctx context.Context,
) (*dagger.Service, error) {

	container, err := client.Container().
		From("registry:2").
		WithExposedPort(5000).
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		AsService().Start(ctx)

	return container, err
}

// Setup Destination Registry as a Dagger Service
func setupDestinationRegistry(
	client *dagger.Client,
	ctx context.Context,
) (*dagger.Service, error) {

	container, err := client.Container().
		From("registry:2").
		WithExposedPort(5000).
		AsService().Start(ctx)

	return container, err
}

// Push image to the Source registry
func pushImageToSourceRegistry(
	t *testing.T,
	ctx context.Context,
	client *dagger.Client,
	source *dagger.Service,
) error {

	container := client.Container().
		From("docker:dind").
		WithServiceBinding("source", source)

	// add crane & push images
	container = container.WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "copy", "public.ecr.aws/docker/library/busybox:1.36", "source:5000/library/busybox:1.36", "--insecure"}).
		WithExec([]string{"crane", "digest", "source:5000/library/busybox:1.36", "--insecure"})

	// check pushed images exist
	container = container.WithExec([]string{"crane", "catalog", "source:5000", "--insecure"})

	stdOut, err := container.Stdout(ctx)
	if strings.Contains(stdOut, "ERROR") {
		return err
	}

	return nil
}

func buildSatellite(
	t *testing.T,
	client *dagger.Client,
	ctx context.Context,
) {
	// Get the directory
	parentDir, err := getProjectDir()
	assert.NoError(t, err, "Failed to get Project Directory")

	// Use the directory path in Dagger
	dir := client.Host().Directory(parentDir)

	config, err := filepath.Abs(filepath.Join(parentDir, test_config_path))
	fmt.Println("the new config path: ", config)

	// Get configuration file on the host
	configFile := client.Host().File(config)

	// Configure and build the Satellite
	container := client.Container().From("golang:alpine").WithDirectory(appDir, dir).
		WithWorkdir(appDir).
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

	fmt.Println("The following is the stdout of the satellite: ")
	fmt.Println()
	fmt.Print(output)
	fmt.Println()
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
