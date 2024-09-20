package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

const (
	DEFAULT_GO          = "golang:1.22"
	PROJ_MOUNT          = "/app"
	DOCKER_PORT         = 2375
	GORELEASER_VERSION  = "v2.1.0"
	GROUND_CONTROL_PATH = "./ground-control"
	SATELLITE_PATH      = "."
)

type HarborSatellite struct{}

// Build function would start the build process for the name provided. Source should be the path to the main.go file.
func (m *HarborSatellite) Build(
	ctx context.Context,
	// +optional
	// +defaultPath="./"
	source *dagger.Directory,
	component string) *dagger.Directory {
	return m.build(source, component)
}

// Release function would release the build to the github with the tags provided. Directory should be "." for both the satellite and the ground control.
func (m *HarborSatellite) Release(ctx context.Context, directory *dagger.Directory, token, name string,
	// +optional
	// +default="patch"
	release_type string) (string, error) {

	container := dag.Container().
		From("alpine/git").
		WithEnvVariable("GITHUB_TOKEN", token).
		WithMountedDirectory(PROJ_MOUNT, directory).
		WithWorkdir(PROJ_MOUNT).
		WithExec([]string{"git", "config", "--global", "url.https://github.com/.insteadOf", "git@github.com:"}).
		WithExec([]string{"git", "fetch", "--tags"})
	// Prepare the tags for the release
	release_tag, err := m.get_release_tag(ctx, container, directory, name, release_type)
	if err != nil {
		slog.Error("Failed to prepare for release: ", err, ".")
		slog.Error("Tag Release Output:", release_tag, ".")
		os.Exit(1)
	}
	slog.Info("Tag Release Output:", release_tag, ".")
	pathToMain, err := m.getPathToReleaser(name)
	if err != nil {
		slog.Error("Failed to get path to main: ", err, ".")
		os.Exit(1)
	}
	release_output, err := container.
		From(fmt.Sprintf("goreleaser/goreleaser:%s", GORELEASER_VERSION)).
		WithMountedDirectory(PROJ_MOUNT, directory).
		WithWorkdir(PROJ_MOUNT).
		WithEnvVariable("GITHUB_TOKEN", token).
		WithEnvVariable("PATH_TO_MAIN", pathToMain).
		WithEnvVariable("APP_NAME", name).
		WithExec([]string{"git", "tag", release_tag}).
		WithExec([]string{"goreleaser", "release", "-f", pathToMain, "--clean"}).
		Stderr(ctx)

	if err != nil {
		slog.Error("Failed to release: ", err, ".")
		slog.Error("Release Output:", release_output, ".")
		os.Exit(1)
	}

	return release_output, nil
}
