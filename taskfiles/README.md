# Task Commands

This project uses [Task](https://taskfile.dev) as the build tool.

## Prerequisites

- Go 1.26.5+
- Task 3.x (`go install github.com/go-task/task/v3/cmd/task@latest`)
- Docker (lint and vulnerability checks run in pinned containers; E2E tests), with buildx for image publishing (Podman also works for `task publish`)
- goreleaser v2 (for releases)
- cosign (for image signing)

## Quick Start

```bash
# List all available tasks
task --list

# Build the satellite, Ground Control server, and Ground Control CLI
task build

# Run linter
task lint

# Run E2E tests
task e2e
```

## Build Tasks

| Command | Description |
|---------|-------------|
| `task build` | Build all three executables for the current platform into `bin/satellite`, `bin/ground-control` (server) and `bin/groundcontrol` (CLI) |
| `task build-all` | Build all three executables for all supported platforms into `bin/<name>/<name>-<os>-<arch>` |
| `task clean` | Remove `bin/`, `dist/` and lint reports, stop E2E and compose containers, remove E2E images |

Single components: `task _build:satellite`, `task _build:ground-control`, `task _build:groundcontrol-cli`.

## Code Generation Tasks

| Command | Description |
|---------|-------------|
| `task generate:ground-control` (`task gen:gc`) | Generate the Ground Control OpenAPI server and client code from `spec/ground-control/openapi.yaml` |
| `task generate:ground-control-server` | Generate the server code only |
| `task generate:ground-control-client` | Generate the client code only |

## Lint Tasks

| Command | Description |
|---------|-------------|
| `task lint` | Run golangci-lint in a pinned container |
| `task lint-fix` | Run golangci-lint with auto-fix |
| `task lint-report` | Run lint and export to `golangci-lint.report` |
| `task vuln` | Run govulncheck (filters known issues) |
| `task vuln-report` | Run govulncheck and export to `vulnerability-check.report` |

CI (`.github/workflows/lint.yaml`) runs golangci-lint v2.12.2 directly.

## Publish Tasks

| Command | Description |
|---------|-------------|
| `task publish DEST=registry/project` | Publish both components to registry (from-source image build) |
| `task publish-and-sign REGISTRY=<registry> PROJECT_NAME=<project> TAG=<tag>` | Build, push and Cosign-sign both images |
| `task snapshot` | Create snapshot release with GoReleaser |
| `task release` | Create official release |

Local image publishing uses `task publish`. CI does **not** use that recipe. `.github/workflows/release.yaml` instead:

1. Cross-compiles `satellite` and `ground-control` per architecture (`task _build:cross-compile`)
2. Builds and pushes each image by digest (`BUILDER_MODE=prebuilt`)
3. Merges the five architecture digests into a multi-arch tag for each of `satellite` and `ground-control`
4. Signs the tagged images with keyless cosign
5. On `vX.Y.Z` tags, creates the GitHub Release (`task release`)

Tagging:

- Push to `main` publishes `satellite:latest` and `ground-control:latest`
- A `vX.Y.Z` tag publishes version tags only and does **not** move `latest`
- `vX.Y.Z` also runs GoReleaser for GitHub Release binaries

### Local examples

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
| `task e2e` | Run `e2e-test`, `e2e-byo` and `e2e-spiffe` in sequence |
| `task e2e-test` | Run main E2E test only |
| `task e2e-byo` | Run BYO registry E2E test |
| `task e2e-spiffe` | Run SPIFFE join token E2E test |
| `task e2e-crash-recovery` | Run crash recovery E2E test with the local OCI store |
| `task e2e-crash-recovery-byo` | Run crash recovery E2E test with a BYO registry |

`task e2e` does not include the crash recovery variants.

## BYO Registry Tasks

| Command | Description |
|---------|-------------|
| `task byo-up` | Start a satellite with a `registry:2` sidecar as its BYO registry (`docker-compose.byo.yml`) |
| `task byo-down` | Stop the BYO registry setup and remove its volumes |

`docker-compose.byo.yml` reads `TOKEN`, `HARBOR_REGISTRY_URL` and `GROUND_CONTROL_URL` from the environment or `.env`.

## Aliases

| Alias | Full Command |
|-------|--------------|
| `task b` | `task build` |
| `task l` | `task lint` |
| `task p` | `task publish` |

## Troubleshooting

### Task not found
Ensure Task is installed: `task --version`

### Lint or vulnerability check fails to start
Both run in Docker. Check Docker is running: `docker ps`

### E2E tests failing
Check Docker is running: `docker ps`
Check logs: `docker compose -f test/e2e/docker/docker-compose.yml logs`
