package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

const (
	DEFAULT_GO = "golang:1.22"
	PROJ_MOUNT = "/app"
	OUT_DIR    = "/binaries"
	DOCKER_PORT =- 2375
)

type HarborSatellite struct{}

func (m *HarborSatellite) Start(ctx context.Context, name string, source *dagger.Directory, release *dagger.Directory, GITHUB_TOKEN, VERSION, REPO_OWNER, REPO_NAME, RELEASE_NAME string) {

	if name == "" {
		slog.Error("Please provide the app name (satellite or ground-control) as an argument")
		os.Exit(1)
	}

	switch name {
	case "satellite":
		slog.Info("Starting satellite CI")
		err := m.StartSatelliteCi(ctx, source, release, GITHUB_TOKEN, VERSION, REPO_OWNER, REPO_NAME, RELEASE_NAME, name)
		if err != nil {
			slog.Error("Failed to start satellite CI")
			os.Exit(1)
		}
	case "ground-control":
		slog.Info("Starting ground-control CI")
		err := m.StartGroundControlCI(ctx, source, release, GITHUB_TOKEN, VERSION, REPO_OWNER, REPO_NAME, RELEASE_NAME, name)
		if err != nil {
			slog.Error("Failed to complete ground-control CI")
			os.Exit(1)
		}
	default:
		slog.Error("Invalid app name. Please provide either 'satellite' or 'ground-control'")
		os.Exit(1)
	}
}

func (m *HarborSatellite) Build(ctx context.Context, source *dagger.Directory, name string) *dagger.Directory {
	return m.build(source, name)
}

func (m *HarborSatellite) Release(ctx context.Context, directory *dagger.Directory, release *dagger.Directory, GITHUB_TOKEN, VERSION, REPO_OWNER, REPO_NAME, RELEASE_NAME, name string) (string, error) {

	releaseContainer := dag.Container().
		From("alpine:latest").
		WithDirectory(".", directory).
		WithDirectory(".", release).
		WithExec([]string{"apk", "add", "--no-cache", "bash", "curl"}).
		WithEnvVariable("GITHUB_API_TOKEN", GITHUB_TOKEN).
		WithEnvVariable("VERSION", fmt.Sprintf("%s-%s", name, VERSION)).
		WithEnvVariable("REPO_OWNER", REPO_OWNER).
		WithEnvVariable("REPO_NAME", REPO_NAME).
		WithEnvVariable("RELEASE_NAME", RELEASE_NAME).
		WithEnvVariable("OUT_DIR", OUT_DIR).
		WithExec([]string{"chmod", "+x", "release.sh"}).
		WithExec([]string{"ls", "-lR", "."}).
		WithExec([]string{"bash", "-c", "./release.sh"})
	output, err := releaseContainer.Stdout(ctx)
	if err != nil {
		return output, err
	}

	// Return the updated release directory
	return output, nil
}

func (m *HarborSatellite) build(source *dagger.Directory, name string) *dagger.Directory {
	fmt.Print("Building Satellite\n")
	gooses := []string{"linux", "darwin"}
	goarches := []string{"amd64", "arm64"}
	binaryName := name // base name for the binary

	// create empty directory to put build artifacts
	outputs := dag.Directory()

	golang := dag.Container().
		From(DEFAULT_GO).
		WithDirectory(PROJ_MOUNT, source).
		WithWorkdir(PROJ_MOUNT)
	for _, goos := range gooses {
		for _, goarch := range goarches {
			// create the full binary name with OS and architecture
			outputBinary := fmt.Sprintf("%s/%s-%s-%s", OUT_DIR, binaryName, goos, goarch)

			// build artifact with specified binary name
			build := golang.
				WithEnvVariable("GOOS", goos).
				WithEnvVariable("GOARCH", goarch).
				WithExec([]string{"go", "build", "-o", outputBinary})

			// add build to outputs
			outputs = outputs.WithDirectory(OUT_DIR, build.Directory(OUT_DIR))
		}
	}

	// return build directory
	return outputs
}
