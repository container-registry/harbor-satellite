package main

import (
	"context"
	"fmt"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

// Would execute the tests for the ground control. Source should be the path to the path to main.go file.
func (m *HarborSatellite) ExecuteTestsForGroundControl(ctx context.Context, source *dagger.Directory) (string, error) {
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
