package main

import (
	"context"
	"dagger/harbor-satellite/internal/dagger"
	"fmt"
	"log"
)

// SnapshotRelease Create snapshot non OCI artifacts with goreleaser
func (m *HarborSatellite) SnapshotRelease(ctx context.Context) *dagger.Directory {
	return m.goreleaserContainer().
		WithExec([]string{"goreleaser", "release", "--snapshot", "--clean"}).
		Directory("/src/dist")
}

// Release Create release with goreleaser
func (m *HarborSatellite) Release(ctx context.Context, githubToken *dagger.Secret) {
	goreleaser := m.goreleaserContainer().
		WithSecretVariable("GITHUB_TOKEN", githubToken).
		WithExec([]string{"goreleaser", "release", "--clean"})
	_, err := goreleaser.Stderr(ctx)
	if err != nil {
		log.Printf("Error occured during release: %s", err)
		return
	}
	log.Println("Release tasks completed successfully 🎉")
}

// Return a container with the goreleaser binary mounted and the source directory mounted.
func (m *HarborSatellite) goreleaserContainer() *dagger.Container {
	return dag.Container().
		From(fmt.Sprintf("goreleaser/goreleaser:%s", GORELEASER_VERSION)).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-"+GO_VERSION)).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-"+GO_VERSION)).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithMountedDirectory("/src", m.Source).
		WithWorkdir("/src").
		WithEnvVariable("TINI_SUBREAPER", "true")
}
