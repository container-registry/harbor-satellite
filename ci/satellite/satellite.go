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

const BinaryPath = "./bin"
const DockerFilePath = "./ci/satellite/Dockerfile"

type SatelliteCI struct {
	config *config.Config
	client *dagger.Client
	ctx    *context.Context
}

func NewSatelliteCI(client *dagger.Client, ctx *context.Context, config *config.Config) *SatelliteCI {
	return &SatelliteCI{
		client: client,
		ctx:    ctx,
		config: config,
	}
}

func (s *SatelliteCI) StartSatelliteCI() error {
	err := s.ExecuteTests()
	if err != nil {
		return err
	}
	err = s.BuildSatellite()
	if err != nil {
		return err
	}
	err = s.ReleaseBinary()
	if err != nil {
		return err
	}
	return nil
}

func (s *SatelliteCI) ExecuteTests() error {
	slog.Info("Running Tests")

	cmd := exec.Command("go", "test", "./...", "-v", "-count=1")

	_, err := cmd.CombinedOutput()

	if err != nil {
		slog.Error("Error executing tests: ", err.Error(), ".")
		return err
	}

	slog.Info("Tests executed successfully")
	return nil
}

func (s *SatelliteCI) BuildSatellite() error {
	slog.Info("Building binary for Satellite")
	currentDir, err := os.Getwd()
	if err != nil {
		slog.Error("Failed to get current directory: ", err.Error(), ".")
		return err
	}
	slog.Info("Current directory: ", currentDir, ".")
	sourceDir := s.client.Host().Directory(currentDir)
	slog.Info("Source directory set")

	binaryBuildContainer := s.client.Container().
		From("golang:1.22").
		WithDirectory("/satellite", sourceDir).
		WithWorkdir("/satellite").
		WithExec([]string{"go", "build", "-o", fmt.Sprintf("/%s/satellite", BinaryPath), "."})

	slog.Info("Build container configured")
	_, err = binaryBuildContainer.File(fmt.Sprintf("/%s/satellite", BinaryPath)).
		Export(*s.ctx, fmt.Sprintf("/%s/%s/satellite", currentDir, BinaryPath))

	if err != nil {
		slog.Error("Failed to export binary: ", err.Error(), ".")
		return err
	}
	slog.Info("Binary built and exported successfully for Satellite")
	return nil
}

func (s *SatelliteCI) ReleaseBinary() error {
	slog.Info("Releasing binary to GitHub")
	ctx := *s.ctx
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: s.config.GithubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	// Create a new release
	release, _, err := client.Repositories.CreateRelease(ctx, s.config.GithubUser, "harbor-satellite", &github.RepositoryRelease{
		TagName:    github.String("v" + s.config.Github_SHA[:5]),
		Name:       github.String(s.config.AppName + " Release"),
		Body:       github.String("Automated release by CI"),
		Draft:      github.Bool(false),
		Prerelease: github.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("failed to create GitHub release: %v", err)
	}

	// Open the binary file
	binaryPath := filepath.Join(BinaryPath, "satellite")
	file, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to open binary file: %v", err)
	}
	defer file.Close()

	// Upload the binary as an asset
	_, _, err = client.Repositories.UploadReleaseAsset(ctx, s.config.GithubUser, "harbor-satellite", *release.ID, &github.UploadOptions{
		Name: "satellite",
	}, file)
	if err != nil {
		return fmt.Errorf("failed to upload release asset: %v", err)
	}

	slog.Info("Binary released successfully to GitHub")
	return nil
}
