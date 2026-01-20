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
// Known unfixable vulnerabilities in zotregistry.dev/zot are filtered out
func (m *HarborSatellite) VulnerabilityCheck(ctx context.Context) (string, error) {
	// Known vulnerabilities with no upstream fix available
	// GO-2025-3409: Zot IdP group membership revocation ignored
	// GO-2024-2979: Zot cache driver blob access without access control
	return m.vulnerabilityCheck(ctx).
		WithExec([]string{"sh", "-c", `
govulncheck -format json ./... 2>&1 | tee /tmp/vuln.json || true
# Check for vulnerabilities excluding known unfixable ones
if grep -q '"id":"GO-' /tmp/vuln.json; then
  # Filter out known zot vulnerabilities
  VULNS=$(grep -o '"id":"GO-[^"]*"' /tmp/vuln.json | grep -v 'GO-2025-3409' | grep -v 'GO-2024-2979' || true)
  if [ -n "$VULNS" ]; then
    echo "Found unexpected vulnerabilities:"
    echo "$VULNS"
    govulncheck -show verbose ./...
    exit 1
  fi
fi
echo "No unexpected vulnerabilities found (known zot vulnerabilities with no fix are excluded)"
`}).
		Stdout(ctx)
}

// Runs a vulnerability check using govulncheck and writes results to vulnerability-check.report
func (m *HarborSatellite) VulnerabilityCheckReport(ctx context.Context) *dagger.File {
	report := "vulnerability-check.report"
	return m.vulnerabilityCheck(ctx).
		WithExec([]string{
			"sh", "-c", fmt.Sprintf("govulncheck ./... > %s 2>&1 || true", report),
		}).File(report)
}
