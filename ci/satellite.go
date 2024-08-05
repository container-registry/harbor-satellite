package main

import (
	"context"
	"fmt"
	"log/slog"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

func (m *HarborSatellite) StartSatelliteCi(ctx context.Context, source *dagger.Directory, release *dagger.Directory, GITHUB_TOKEN, VERSION, REPO_OWNER, REPO_NAME, RELEASE_NAME , name string) error {
	// Build Satellite
	slog.Info("Building Satellite")
	outputDir := m.Build(ctx, source, name)

	release_output, err := m.Release(ctx, outputDir, release, GITHUB_TOKEN, VERSION, REPO_OWNER, REPO_NAME, RELEASE_NAME, name)
	if err != nil {
		slog.Error("Failed to release Satellite")
		slog.Error(err.Error())
		slog.Error((fmt.Sprintf("Release Directory: %s", release_output)))
		return err
	}
	return nil
}

func (m *HarborSatellite) ExecuteTestsForSatellite(ctx context.Context, source *dagger.Directory) (string, error) {
	goContainer := dag.Container().
		From(DEFAULT_GO)

	containerWithDocker, err := m.Attach(ctx, goContainer, "24.0")
	if err != nil {
		return "", err
	}
	dockerHost, err := containerWithDocker.EnvVariable(ctx, "DOCKER_HOST")
	if err != nil {
		return "", err
	}
	fmt.Printf("Docker Host: %s\n", dockerHost)

	goContainer = containerWithDocker.
		WithMountedDirectory("/app", source).
		WithWorkdir("/app").
		WithExec([]string{"go", "test", "./..."})

	output, err := goContainer.Stdout(ctx)
	if err != nil {
		return output, err
	}
	fmt.Print(output)
	return output, nil
}
