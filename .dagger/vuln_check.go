package main

import (
	"context"
	"dagger/harbor-satellite/internal/dagger"
	"fmt"
)

// Checks for vulnerabilities using govulncheck
func (m *HarborSatellite) vulnerabilityCheck(ctx context.Context) *dagger.Container {
	return dag.Container().
		From("golang:"+GO_VERSION+"-alpine").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-"+GO_VERSION)).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-"+GO_VERSION)).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithExec([]string{"go", "install", "golang.org/x/vuln/cmd/govulncheck@latest"}).
		WithMountedDirectory("/src", m.Source).
		WithWorkdir("/src")
}

// Runs a vulnerability check using govulncheck
func (m *HarborSatellite) VulnerabilityCheck(ctx context.Context) (string, error) {
	return m.vulnerabilityCheck(ctx).
		WithExec([]string{"govulncheck", "-show", "verbose", "./..."}).
		Stderr(ctx)
}

// Runs a vulnerability check using govulncheck and writes results to vulnerability-check.report
func (m *HarborSatellite) VulnerabilityCheckReport(ctx context.Context) *dagger.File {
	report := "vulnerability-check.report"
	return m.vulnerabilityCheck(ctx).
		WithExec([]string{
			"sh", "-c", fmt.Sprintf("govulncheck ./... > %s 2>&1 || true", report),
		}).File(report)
}
