package main

import (
	"context"
	"fmt"
	"log/slog"

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
func (m *HarborSatellite) Build(ctx context.Context, source *dagger.Directory, name string) *dagger.Directory {
	return m.build(source, name)
}

// Release function would release the build to the github with the tags provided. Directory should be "." for both the satellite and the ground control.
func (m *HarborSatellite) Release(ctx context.Context, directory *dagger.Directory, token, name string) (string, error) {
	var path_to_main string

	if name == "satellite" {
		path_to_main = ".goreleaser.yaml"
	} else {
		path_to_main = "ground-control/.goreleaser.yaml"
	}
	release_output, err := dag.Container().
		From(fmt.Sprintf("goreleaser/goreleaser:%s", GORELEASER_VERSION)).
		WithMountedDirectory(PROJ_MOUNT, directory).
		WithWorkdir(PROJ_MOUNT).
		WithEnvVariable("GITHUB_TOKEN", token).
		WithEnvVariable("PATH_TO_MAIN", path_to_main).
		WithEnvVariable("APP_NAME", name).
		WithExec([]string{"goreleaser", "release", "-f", path_to_main, "--clean"}).
		Stderr(ctx)

	if err != nil {
		slog.Error("Failed to release Ground Control: ", err, ".")
		slog.Error("Release Output:", release_output, ".")
		return release_output, err
	}

	return release_output, nil
}

func (m *HarborSatellite) build(source *dagger.Directory, name string) *dagger.Directory {
	fmt.Printf("Building %s\n", name)
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
			outputBinary := fmt.Sprintf("%s/%s-%s-%s", name, binaryName, goos, goarch)

			// build artifact with specified binary name
			build := golang.
				WithEnvVariable("GOOS", goos).
				WithEnvVariable("GOARCH", goarch).
				WithExec([]string{"go", "build", "-o", outputBinary})

			// add build to outputs
			outputs = outputs.WithDirectory(name, build.Directory(name))
		}
	}

	// return build directory
	return outputs
}
