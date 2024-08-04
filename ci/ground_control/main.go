package ground_control_ci

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

const (
	// Update BinaryPath to match the expected path in Dockerfile or your build setup
	BinaryPath     = "./"           // Adjust if a specific directory is used for binaries
	DockerFilePath = "./Dockerfile" // Assuming Dockerfile is in the root directory
)

// GroundControlCI holds the configuration and context for the CI process
type GroundControlCI struct {
	config *config.Config
	client *dagger.Client
	ctx    context.Context
}

// NewGroundControlCI creates a new instance of GroundControlCI
func NewGroundControlCI(client *dagger.Client, ctx context.Context, config *config.Config) *GroundControlCI {
	return &GroundControlCI{
		client: client,
		ctx:    ctx,
		config: config,
	}
}

// StartGroundControlCI starts the CI process for Ground Control
func (s *GroundControlCI) StartGroundControlCI() error {
	// Execute tests
	if err := s.ExecuteTests(); err != nil {
		return err
	}

	// Build the Ground Control binaries
	if err := s.BuildGroundControl(); err != nil {
		return err
	}

	// Release the built binaries
	if err := s.ReleaseBinary(); err != nil {
		return err
	}

	return nil
}

// ExecuteTests runs the tests for the Ground Control project
func (s *GroundControlCI) ExecuteTests() error {
	slog.Info("Running Tests")

	cmd := exec.Command("go", "test", "./...", "-v", "-count=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("Error executing tests: ", err.Error(), ".")
		slog.Error("Output: ", string(output), ".")
		return err
	}

	slog.Info("Output: ", string(output), ".")
	slog.Info("Tests executed successfully.")
	return nil
}

// BuildGroundControl builds the Ground Control binaries using Dagger
func (s *GroundControlCI) BuildGroundControl() error {
	slog.Info("Building binaries for Ground Control")

	currentDir, err := os.Getwd()
	if err != nil {
		return err
	}
	slog.Info("Current directory: ", currentDir, ".")
	groundControlDir := filepath.Join(currentDir, "ground-control")
	sourceDir := s.client.Host().Directory(groundControlDir)
	slog.Info("Source directory set")

	// List of binaries to build
	binaries := []string{"ground_control", "another_binary"}

	for _, binary := range binaries {
		slog.Info("Building binary: ", binary)

		binaryBuildContainer := s.client.Container().
			From("golang:1.22").
			WithDirectory("/app", sourceDir).
			WithWorkdir("/app").
			WithExec([]string{"go", "build", "-o", filepath.Join(BinaryPath, binary), "."})

		slog.Info("Build container configured for: ", binary)

		_, err := binaryBuildContainer.File(filepath.Join(BinaryPath, binary)).
			Export(s.ctx, filepath.Join(currentDir, BinaryPath, binary))

		if err != nil {
			return err
		}
		slog.Info("Binary built and exported successfully for: ", binary)
	}

	return nil
}

// ReleaseBinary releases the built binaries to GitHub
func (s *GroundControlCI) ReleaseBinary() error {
	slog.Info("Releasing binaries to GitHub")

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: s.config.GithubToken},
	)
	tc := oauth2.NewClient(s.ctx, ts)
	client := github.NewClient(tc)

	// Create a new release
	release, _, err := client.Repositories.CreateRelease(s.ctx, s.config.GithubUser, "harbor-satellite", &github.RepositoryRelease{
		TagName:    github.String("v" + s.config.Github_SHA[:6]),
		Name:       github.String(s.config.AppName + " Release"),
		Body:       github.String("Automated release by CI"),
		Draft:      github.Bool(false),
		Prerelease: github.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("failed to create GitHub release: %v", err)
	}

	// List of binaries to release
	binaries := []string{"ground_control", "another_binary"}

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
