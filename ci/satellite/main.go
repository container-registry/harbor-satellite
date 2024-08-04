package satellite_ci

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/oauth2"

	"container-registry.com/harbor-satellite/ci/config"
	"dagger.io/dagger"
	"github.com/google/go-github/v39/github"
)

const BinaryPath = "./"
const DockerFilePath = "./ci/satellite/Dockerfile"

// SatelliteCI holds the configuration and context for the CI process
type SatelliteCI struct {
	config *config.Config
	client *dagger.Client
	ctx    context.Context
}

// NewSatelliteCI creates a new instance of SatelliteCI
func NewSatelliteCI(client *dagger.Client, ctx context.Context, config *config.Config) *SatelliteCI {
	return &SatelliteCI{
		client: client,
		ctx:    ctx,
		config: config,
	}
}

// StartSatelliteCI starts the CI process for Satellite
func (s *SatelliteCI) StartSatelliteCI() error {
	// Execute tests
	if err := s.ExecuteTests(); err != nil {
		return err
	}

	// Build the Satellite binaries
	if err := s.BuildSatellite(); err != nil {
		return err
	}

	// Release the built binaries
	if err := s.ReleaseBinary(); err != nil {
		return err
	}

	return nil
}

// ExecuteTests runs the tests for the Satellite project
func (s *SatelliteCI) ExecuteTests() error {
	slog.Info("Running Tests")

	cmd := exec.Command("go", "test", "./...", "-v", "-count=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("Error executing tests: ", "error", err.Error())
		slog.Error("Output: ", "output", string(output))
		return err
	}

	slog.Info("Output: ", string(output))
	slog.Info("Tests executed successfully")
	return nil
}

// BuildSatellite builds the Satellite binaries using Dagger
func (s *SatelliteCI) BuildSatellite() error {
	slog.Info("Building binaries for Satellite")

	currentDir, err := os.Getwd()
	if err != nil {
		slog.Error("Failed to get current directory: ", err.Error())
		return err
	}
	slog.Info("Current directory: ", currentDir)

	sourceDir := s.client.Host().Directory(currentDir)
	slog.Info("Source directory set")

	// List of binaries to build
	binaries := []string{"satellite", "another_binary"}

	for _, binary := range binaries {
		slog.Info("Building binary: ", binary)

		binaryBuildContainer := s.client.Container().
			From("golang:1.22").
			WithDirectory("/satellite", sourceDir).
			WithWorkdir("/satellite").
			WithExec([]string{"go", "build", "-o", fmt.Sprintf("/%s/%s", BinaryPath, binary), "."})

		slog.Info("Build container configured for: ", binary)

		_, err := binaryBuildContainer.File(fmt.Sprintf("/%s/%s", BinaryPath, binary)).
			Export(s.ctx, fmt.Sprintf("%s/%s", BinaryPath, binary))

		if err != nil {
			return err
		}
		slog.Info("Binary built and exported successfully for: ", binary)
	}

	return nil
}

// ReleaseBinary releases the built binaries to GitHub
func (s *SatelliteCI) ReleaseBinary() error {
	slog.Info("Releasing binaries to GitHub")

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: s.config.GithubToken},
	)
	tc := oauth2.NewClient(s.ctx, ts)
	client := github.NewClient(tc)

	// Create a new release
	release, _, err := client.Repositories.CreateRelease(s.ctx, s.config.GithubUser, "harbor-satellite", &github.RepositoryRelease{
		TagName:    github.String("v" + s.config.Github_SHA[:5]),
		Name:       github.String(s.config.AppName + " Release"),
		Body:       github.String("Automated release by CI"),
		Draft:      github.Bool(false),
		Prerelease: github.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("failed to create GitHub release: %v", err)
	}

	// List of binaries to release
	binaries := []string{"satellite", "another_binary"}

	for _, binary := range binaries {
		binaryPath := filepath.Join(BinaryPath, binary)
		file, err := os.Open(binaryPath)
		if err != nil {
			return fmt.Errorf("failed to open binary file: %v", err)
		}
		defer file.Close()

		// Upload the binary as an asset
		_, _, err = client.Repositories.UploadReleaseAsset(s.ctx, s.config.GithubUser, "harbor-satellite", *release.ID, &github.UploadOptions{
			Name: binary,
		}, file)
		if err != nil {
			return fmt.Errorf("failed to upload release asset: %v", err)
		}

		slog.Info("Binary released successfully to GitHub: ", binary)
	}

	return nil
}
