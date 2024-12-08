package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

const (
	DEFAULT_GO          = "golang:1.22"
	PROJ_MOUNT          = "/app"
	GO_VERSION          = "1.22"
	DOCKER_PORT         = 2375
	GORELEASER_VERSION  = "v2.4.8"
	GROUND_CONTROL_PATH = "./ground-control"
	SATELLITE_PATH      = "."
	DOCKER_VERSION      = "24.0"
)

func New(
	// Local or remote directory with source code, defaults to "./"
	// +optional
	// +defaultPath="./"
	Source *dagger.Directory,
) *HarborSatellite {
	return &HarborSatellite{
		Source: Source,
	}
}

type HarborSatellite struct {
	// Local or remote directory with source code, defaults to "./"
	// +defaultPath="./"
	Source *dagger.Directory
}

// start the dev server for ground-control.
func (m *HarborSatellite) RunGroundControl(
	ctx context.Context,
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
) (*dagger.Service, error) {
	golang := dag.Container().
		From("golang:latest").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithMountedDirectory(PROJ_MOUNT, source).
		WithWorkdir(PROJ_MOUNT).
		WithExec([]string{"go", "install", "github.com/air-verse/air@latest"})

	db, err := m.Db(ctx)
	if err != nil {
		return nil, err
	}

	golang = golang.
		WithWorkdir(PROJ_MOUNT+"/ground-control/sql/schema").
		WithExec([]string{"ls", "-la"}).
		WithServiceBinding("pgservice", db).
		WithExec([]string{"go", "install", "github.com/pressly/goose/v3/cmd/goose@latest"}).
		WithExec([]string{"goose", "postgres", "postgres://postgres:password@pgservice:5432/groundcontrol", "up"}).
		WithWorkdir(PROJ_MOUNT + "/ground-control").
		WithExec([]string{"ls", "-la"}).
		WithExec([]string{"go", "mod", "download"}).
		WithExec([]string{"air", "-c", ".air.toml"}).
		WithExposedPort(8080)

	return golang.AsService(), nil
}

// quickly build binaries for components for given platform.
func (m *HarborSatellite) BuildDev(
	ctx context.Context,
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
	platform string,
	component string,
) (*dagger.File, error) {
	fmt.Println("🛠️  Building Harbor-Cli with Dagger...")
	// Define the path for the binary output
	os, arch, err := parsePlatform(platform)
	if err != nil {
		log.Fatalf("Error parsing platform: %v", err)
	}

	if component == "satellite" || component == "ground-control" {
		var binaryFile *dagger.File
		golang := dag.Container().
			From("golang:latest").
			WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
			WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
			WithMountedCache("/go/build-cache", dag.CacheVolume("go-build")).
			WithEnvVariable("GOCACHE", "/go/build-cache").
			WithMountedDirectory(PROJ_MOUNT, source).
			WithWorkdir(PROJ_MOUNT).
			WithEnvVariable("GOOS", os).
			WithEnvVariable("GOARCH", arch)

		if component == "ground-control" {
			golang = golang.
				WithWorkdir(PROJ_MOUNT + "/ground-control").
				WithExec([]string{"ls", "-la"}).
				WithExec([]string{"go", "mod", "download"}).
				WithExec([]string{"go", "build", "."})

			binaryFile = golang.File(PROJ_MOUNT + "/ground-control/ground-control")
		} else {
			golang = golang.
				WithExec([]string{"ls", "-la"}).
				WithExec([]string{"go", "mod", "download"}).
				WithExec([]string{"go", "build", "."})

			binaryFile = golang.File(PROJ_MOUNT + "/harbor-satellite")
		}

		return binaryFile, nil
	}

	return nil, fmt.Errorf("error: please provide component as either satellite or ground-control")
}

// starts postgres DB container for ground-control.
func (m *HarborSatellite) Db(ctx context.Context) (*dagger.Service, error) {
	return dag.Container().
		From("postgres:17").
		WithEnvVariable("POSTGRES_USER", "postgres").
		WithEnvVariable("POSTGRES_PASSWORD", "password").
		WithEnvVariable("POSTGRES_HOST_AUTH_METHOD", "trust").
		WithEnvVariable("POSTGRES_DB", "groundcontrol").
		WithExposedPort(5432).
		AsService().Start(ctx)
}

// Build function would start the build process for the name provided.
func (m *HarborSatellite) Build(
	ctx context.Context,
	// +optional
	// +defaultPath="./"
	source *dagger.Directory,
	component string,
) (*dagger.Directory, error) {
	var directory *dagger.Directory
	switch {
	case component == "satellite":
		directory = source
	case component == "ground-control":
		directory = source.Directory(GROUND_CONTROL_PATH)
	default:
		return nil, fmt.Errorf("unknown component: %s", component)
	}
	return m.build(directory, component), nil
}

// Release function would release the build to the github with the tags provided.
func (m *HarborSatellite) Release(ctx context.Context,
	// +optional
	// +defaultPath="./"
	source *dagger.Directory,
	githubToken *dagger.Secret,
	component string,
	// +optional
	// +default="patch"
	release_type string,
) (string, error) {
	token, err := githubToken.Plaintext(ctx)
	if err != nil {
		fmt.Println("Failed to get github token: ", err)
		os.Exit(1)
	}
	// trim any whitespace from the token, found a few problems with using the token directly from the secret.
	token = strings.TrimSpace(token)
	container := dag.Container().
		From("alpine/git").
		WithEnvVariable("GITHUB_TOKEN", token).
		WithMountedDirectory(PROJ_MOUNT, source).
		WithWorkdir(PROJ_MOUNT).
		WithExec([]string{"git", "config", "--global", "url.https://github.com/.insteadOf", "git@github.com:"}).
		WithExec([]string{"git", "fetch", "--tags"})
	// Prepare the tags for the release
	release_tag, err := m.get_release_tag(ctx, container, source, component, release_type)
	if err != nil {
		fmt.Println("Failed to get release tag: ", err)
		fmt.Println("Release Tag Output:", release_tag)
		os.Exit(1)
	}
	slog.Info("Tag Release Output:", release_tag, ".")
	pathToMain, err := m.getPathToReleaser(component)
	if err != nil {
		fmt.Println("Failed to get path to main: ", err)
		os.Exit(1)
	}
	release_output, err := container.
		From(fmt.Sprintf("goreleaser/goreleaser:%s", GORELEASER_VERSION)).
		WithMountedDirectory(PROJ_MOUNT, source).
		WithWorkdir(PROJ_MOUNT).
		WithEnvVariable("GITHUB_TOKEN", token).
		WithEnvVariable("PATH_TO_MAIN", pathToMain).
		WithEnvVariable("APP_NAME", component).
		WithExec([]string{"git", "tag", release_tag}).
		WithExec([]string{"goreleaser", "release", "-f", pathToMain, "--clean"}).
		Stderr(ctx)
	if err != nil {
		fmt.Println("Failed to release: ", err)
		fmt.Println("Release Output:", release_output)
		os.Exit(1)
	}

	return release_output, nil
}

// Parse the platform string into os and arch
func parsePlatform(platform string) (string, string, error) {
	parts := strings.Split(platform, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid platform format: %s. Should be os/arch. E.g. darwin/amd64", platform)
	}
	return parts[0], parts[1], nil
}
