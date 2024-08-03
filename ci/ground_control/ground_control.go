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

const BinaryPath = "./bin"
const DockerFilePath = "./ci/ground_control/Dockerfile"

type GroundControlCI struct {
	config *config.Config
	client *dagger.Client
	ctx    *context.Context
}

func NewGroundControlCI(client *dagger.Client, ctx *context.Context, config *config.Config) *GroundControlCI {
	return &GroundControlCI{
		client: client,
		ctx:    ctx,
		config: config,
	}
}

func (s *GroundControlCI) StartGroundControlCI() error {
	err := s.ExecuteTests()
	if err != nil {
		return err
	}
	err = s.BuildGroundControl()
	if err != nil {
		return err
	}
	err = s.ReleaseBinary()
	if err != nil {
		return err
	}

	return nil
}

func (s *GroundControlCI) ExecuteTests() error {
	slog.Info("Running Tests")

	cmd := exec.Command("go", "test", "./...", "-v", "-count=1")

	output, err := cmd.CombinedOutput()

	if err != nil {
		slog.Error("Error executing tests: ", err.Error(), ".")
		slog.Error("Output: ", output, ".")
		return err
	}
	slog.Info("Output: ", output, ".")
	slog.Info("Tests executed successfully.")
	return nil
}

func (s *GroundControlCI) BuildGroundControl() error {
	slog.Info("Building binary for Ground Control")
	currentDir, err := os.Getwd()
	if err != nil {
		return err
	}
	slog.Info("Current directory: ", currentDir, ".")
	groundControlDir := fmt.Sprintf("%s/%s", currentDir, "ground-control")
	sourceDir := s.client.Host().Directory(groundControlDir)
	slog.Info("Source directory set")

	binaryBuildContainer := s.client.Container().
		From("golang:1.22").
		WithDirectory("/ground_control", sourceDir).
		WithWorkdir("/ground_control").
		WithExec([]string{"go", "build", "-o", fmt.Sprintf("/%s/ground_control", BinaryPath), "."})

	slog.Info("Build container configured")
	_, err = binaryBuildContainer.File(fmt.Sprintf("/%s/ground_control", BinaryPath)).
		Export(*s.ctx, fmt.Sprintf("/%s/%s/ground_control", currentDir, BinaryPath))

	if err != nil {
		return err
	}
	slog.Info("Binary built and exported successfully for Ground Control")
	return nil
}

func (s *GroundControlCI) ReleaseBinary() error {
	slog.Info("Releasing binary to GitHub")
	ctx := *s.ctx
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: s.config.GithubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	// Create a new release
	release, _, err := client.Repositories.CreateRelease(ctx, s.config.GithubUser, "harbor-satellite", &github.RepositoryRelease{
		TagName:    github.String("v" + s.config.Github_SHA[:6]),
		Name:       github.String(s.config.AppName + " Release"),
		Body:       github.String("Automated release by CI"),
		Draft:      github.Bool(false),
		Prerelease: github.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("failed to create GitHub release: %v", err)
	}

	// Open the binary file
	binaryPath := filepath.Join(BinaryPath, "ground_control")
	file, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to open binary file: %v", err)
	}
	defer file.Close()

	// Upload the binary as an asset
	_, _, err = client.Repositories.UploadReleaseAsset(ctx, s.config.GithubUser, "harbor-satellite", *release.ID, &github.UploadOptions{
		Name: "ground_control",
	}, file)
	if err != nil {
		return fmt.Errorf("failed to upload release asset: %v", err)
	}

	slog.Info("Binary released successfully to GitHub")
	return nil
}
