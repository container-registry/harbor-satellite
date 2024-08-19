package main

import (
	"context"
	"fmt"
	"log/slog"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

func (m *HarborSatellite) StartSatelliteCi(ctx context.Context, source *dagger.Directory, GITHUB_TOKEN, name string) error {
	// Build Satellite
	slog.Info("Building Satellite")
	_ = m.Build(ctx, source, name)

	release_output, err := m.Release(ctx, source, GITHUB_TOKEN, name)
	if err != nil {
		slog.Error("Failed to release Ground Control: ", err, ".")
		slog.Error("Release Directory:", release_output, ".")
		return err
	}
	return nil
}

func (m *HarborSatellite) ExecuteTestsForSatellite(ctx context.Context, source *dagger.Directory) (string, error) {
	goContainer := dag.Container().
		From("golang:1.22-alpine").
		WithExec([]string{"apk", "add", "--no-cache", "docker"})

	containerWithDocker, err := m.Attach(ctx, goContainer, "24.0")
	if err != nil {
		return "", fmt.Errorf("failed to attach to container: %w", err)
	}
	dockerHost, err := containerWithDocker.EnvVariable(ctx, "DOCKER_HOST")
	if err != nil {
		return "", fmt.Errorf("failed to get DOCKER_HOST: %w", err)
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
