package main

import (
	"context"
	"fmt"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

// Would execute the tests for the satellite. Source should be the path to the path to main.go file.
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
		WithMountedDirectory(PROJ_MOUNT, source).
		WithWorkdir(PROJ_MOUNT).
		WithExec([]string{"go", "test", "./..."})

	output, err := goContainer.Stdout(ctx)
	if err != nil {
		return output, err
	}
	fmt.Print(output)
	return output, nil
}
