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

# Build both components
task build

# Run linter
task lint

# Run E2E tests
task e2e
```

## Build Tasks

| Command | Description |
|---------|-------------|
| `task build` | Build both components for current platform |
| `task build-all` | Build both components for all platforms |

## Lint Tasks

| Command | Description |
|---------|-------------|
| `task lint` | Run golangci-lint |
| `task lint-report` | Run lint and export to file |
| `task vuln` | Run govulncheck (filters known issues) |
| `task vuln-report` | Run govulncheck and export to file |

## Publish Tasks

| Command | Description |
|---------|-------------|
| `task publish DEST=registry/project` | Publish both components to registry |
| `task snapshot` | Create snapshot release with GoReleaser |
| `task release` | Create official release |

### Examples

```bash
# Publish to ttl.sh (anonymous registry, no auth needed)
task publish DEST=ttl.sh/my-test

# Publish with custom tag
task publish DEST=ttl.sh/my-test TAG=v1.0.0

# Publish to private registry
REG_USER=user REG_PASS=pass task publish DEST=ghcr.io/myorg/project
```

## E2E Test Tasks

| Command | Description |
|---------|-------------|
| `task e2e` | Run all E2E tests |
| `task e2e-test` | Run main E2E test only |
| `task e2e-spiffe` | Run SPIFFE join token E2E test |

## Aliases

| Alias | Full Command |
|-------|--------------|
| `task b` | `task build` |
| `task l` | `task lint` |
| `task p` | `task publish` |

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
