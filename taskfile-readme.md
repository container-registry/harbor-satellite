# Task Commands

This project uses [Task](https://taskfile.dev) as the build tool.

## Prerequisites

- Go 1.24.11+
- Task 3.x (`go install github.com/go-task/task/v3/cmd/task@latest`)
- Docker with buildx (for image publishing)
- golangci-lint v2.0.2 (for linting)
- govulncheck (for vulnerability scanning)
- goreleaser v2.9.0 (for releases)
- cosign (for image signing)

## Quick Start

```bash
# List all available tasks
task --list

# Build satellite
task build:satellite

# Build ground-control
task build:ground-control

# Run linter
task lint:lint
```

## Build Tasks

| Command | Description |
|---------|-------------|
| `task build:satellite` | Build satellite binary for current platform |
| `task build:ground-control` | Build ground-control binary for current platform |
| `task build:dev` | Quick dev build for both components |
| `task build:all` | Build both components for all platforms |

### Examples

```bash
# Quick dev build (both components, current platform)
task build:dev

# Build all platforms
task build:all
```

## Lint Tasks

| Command | Description |
|---------|-------------|
| `task lint:lint` | Run golangci-lint |
| `task lint:lint-report` | Run lint and export to golangci-lint.report |
| `task lint:vulnerability-check` | Run govulncheck (filters known issues) |
| `task lint:vulnerability-check-report` | Run govulncheck and export to file |

## Publish Tasks

| Command | Description |
|---------|-------------|
| `task publish:image COMPONENT=satellite` | Build and push multi-arch image |
| `task publish:image-and-sign COMPONENT=satellite` | Build, push, and sign image |
| `task publish:snapshot-release` | Create snapshot release with GoReleaser |
| `task publish:release` | Create official release |

### Required Environment Variables

- `REGISTRY` - Container registry address
- `REGISTRY_USERNAME` - Registry username
- `REGISTRY_PASSWORD` - Registry password
- `PROJECT_NAME` - Project name in registry
- `IMAGE_TAGS` - Comma-separated tags (e.g., "latest,v1.0.0")
- `GITHUB_TOKEN` - For releases

### Examples

```bash
# Publish satellite with tag
REGISTRY=ghcr.io REGISTRY_USERNAME=user REGISTRY_PASSWORD=pass \
  PROJECT_NAME=myorg/harbor-satellite IMAGE_TAGS=latest \
  task publish:image COMPONENT=satellite
```

## E2E Test Tasks

| Command | Description |
|---------|-------------|
| `task e2e:test` | Run full E2E test suite |
| `task e2e:test-spiffe` | Run SPIFFE join token E2E test |
| `task e2e:cleanup` | Clean up E2E infrastructure |

### E2E Test Flow

1. Starts Harbor registry stack (PostgreSQL, Redis, Registry, Core, JobService)
2. Starts Ground Control with PostgreSQL
3. Initializes Harbor with project and replication policy
4. Registers satellite and starts local Zot registry
5. Verifies image replication from Harbor to local Zot

## Task Variables

Override any variable at runtime:

```bash
task publish:image COMPONENT=satellite IMAGE_TAGS=v1.0.0
```

## Troubleshooting

### Task not found
Ensure Task is installed: `task --version`

### golangci-lint not found
Install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@v2.0.2`

### govulncheck not found
Install: `go install golang.org/x/vuln/cmd/govulncheck@latest`

### E2E tests failing
Check Docker is running: `docker ps`
Check logs: `docker compose -f docker/e2e/docker-compose.yml logs`
