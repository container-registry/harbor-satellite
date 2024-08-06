package main

import (
	"context"
	"fmt"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

func (m *HarborSatellite) Attach(ctx context.Context, container *dagger.Container, dockerVersion string) (*dagger.Container, error) {
	dockerd := m.Service(dockerVersion)

	dockerd, err := dockerd.Start(ctx)
	if err != nil {
		return nil, err
	}

	dockerHost, err := dockerd.Endpoint(ctx, dagger.ServiceEndpointOpts{
		Scheme: "tcp",
	})
	if err != nil {
		return nil, err
	}

	return container.
		WithServiceBinding("docker", dockerd).
		WithEnvVariable("DOCKER_HOST", dockerHost), nil
}

// Get a Service container running dockerd
func (m *HarborSatellite) Service(
	// +optional
	// +default="24.0"
	dockerVersion string,
) *dagger.Service {
	port := 2375
	return dag.Container().
		From(fmt.Sprintf("docker:%s-dind", dockerVersion)).
		WithMountedCache(
			"/var/lib/docker",
			dag.CacheVolume(dockerVersion+"-docker-lib"),
			dagger.ContainerWithMountedCacheOpts{
				Sharing: dagger.Private,
			}).
		WithExposedPort(port).
		WithExec([]string{
			"dockerd",
			"--host=tcp://0.0.0.0:2375",
			"--host=unix:///var/run/docker.sock",
			"--tls=false",
		}, dagger.ContainerWithExecOpts{
			InsecureRootCapabilities: true,
		}).
		AsService()
}
