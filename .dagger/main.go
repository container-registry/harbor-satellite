package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"dagger/harbor-satellite/internal/dagger"
)

const (
	DEFAULT_GO           = "golang:1.24.3"
	PROJ_MOUNT           = "/app"
	GO_VERSION           = "1.24.3"
	DOCKER_PORT          = 2375
	GORELEASER_VERSION   = "v2.9.0"
	GOLANGCILINT_VERSION = "v2.0.2"
	GROUND_CONTROL_PATH  = "./ground-control"
	MIGRATOR_PATH        = GROUND_CONTROL_PATH + "/migrator"
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

	gcAuthToken string
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
	fmt.Println("🛠️  Building Harbor-Satellite with Dagger...")
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
				WithExec([]string{"go", "build", "-o", "/ground-control", "./main.go"})

			binaryFile = golang.File("/ground-control")
		} else {
			golang = golang.
				WithWorkdir(PROJ_MOUNT + "/cmd").
				WithExec([]string{"ls", "-la"}).
				WithExec([]string{"go", "mod", "download"}).
				WithExec([]string{"go", "build", "-o", "/harbor-satellite", "./main.go"})

			binaryFile = golang.File("/harbor-satellite")
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

// Executes Linter and writes results to a file golangci-lint.report
func (m *HarborSatellite) LintReport(ctx context.Context) *dagger.File {
	report := "golangci-lint.report"
	return m.lint(ctx).WithExec([]string{
		"golangci-lint", "run", "-v",
		"--output.tab.path=" + report,
		"--issues-exit-code", "0",
	}).File(report)
}

// Lint Run the linter golangci-lint
func (m *HarborSatellite) Lint(ctx context.Context) (string, error) {
	return m.lint(ctx).WithExec([]string{"golangci-lint", "run"}).Stderr(ctx)
}

func (m *HarborSatellite) lint(_ context.Context) *dagger.Container {
	fmt.Println("👀 Running linter and printing results to file golangci-lint.txt.")
	linter := dag.Container().
		From("golangci/golangci-lint:"+GOLANGCILINT_VERSION+"-alpine").
		WithMountedCache("/lint-cache", dag.CacheVolume("/lint-cache")).
		WithEnvVariable("GOLANGCI_LINT_CACHE", "/lint-cache").
		WithMountedDirectory("/src", m.Source).
		WithWorkdir("/src")
	return linter
}

// Parse the platform string into os and arch
func parsePlatform(platform string) (string, string, error) {
	parts := strings.Split(platform, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid platform format: %s. Should be os/arch. E.g. darwin/amd64", platform)
	}
	return parts[0], parts[1], nil
}
