# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Harbor Satellite is a registry fleet management and artifact distribution solution that extends Harbor container registry to edge computing environments. Two main components:

1. Satellite: Runs at edge locations as a lightweight, standalone registry (primary for local workloads, fallback for central Harbor)
2. Ground Control: Cloud-side management service for device management, onboarding, state management, and artifact orchestration

## Build and Development Commands

### Building

```bash
# Build satellite binary (Dagger)
dagger call build --source=. --component=satellite export --path=./bin

# Build ground-control binary (Dagger)
dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev

# Run satellite directly
go run cmd/main.go --token "<token>" --ground-control-url "<url>"

# Run ground-control directly
cd ground-control && go run main.go
```

### Testing

```bash
# Run all tests (with Dagger)
dagger run go test ./... -v -count=1

# Run all tests (without Dagger)
go test ./... -v -count=1 -args -abs=false

# Run a single test
go test -v -run TestFunctionName ./path/to/package

# Run E2E tests
dagger call test-end-to-end
```

### Linting

```bash
dagger call lint-report export --path=golangci-lint.report
```

Uses strict golangci-lint with 50+ linters (see golangci.yaml). Key rules: no global variables (gochecknoglobals), no init functions (gochecknoinits), cyclomatic complexity limits, function length limits (100 lines, 50 statements).

### Running Locally

```bash
# Satellite with Docker Compose
docker compose up -d

# Satellite with Go
go run cmd/main.go --token "<token>" --ground-control-url "http://127.0.0.1:8080"

# Satellite with mirror config
go run cmd/main.go --token "<token>" --ground-control-url "<url>" --mirrors=containerd:docker.io,quay.io

# Ground Control with Docker Compose
cd ground-control && docker compose up

# Ground Control with Go (requires .env file)
cd ground-control && go run main.go
```

## Architecture

### Two-Module Structure

This repository contains two separate Go modules:
- Root module (go.mod): Satellite component
- ground-control/go.mod: Ground Control component

When making changes, be aware which module you're working in. Dependencies and imports are separate.

### Satellite Component Structure

- cmd/main.go: Entry point, handles CLI flags (token, ground-control-url, mirrors, json-logging)
- pkg/config/: Configuration management, validation, hot-reloading
- internal/satellite/: Core orchestration logic
- internal/state/: State management (replication, fetching, artifact handling, registration)
- internal/registry/: Local OCI registry management (Zot integration)
- internal/scheduler/: Cron-based job scheduling
- internal/container_runtime/: CRI config management (Docker, containerd, CRI-O, Podman)
- internal/server/: HTTP server for metrics and health
- internal/watcher/: Config file watching for hot-reload
- internal/hotreload/: Hot-reload mechanism

### Ground Control Component Structure

- ground-control/main.go: Entry point, checks Harbor health, starts server
- ground-control/internal/server/: HTTP API handlers (satellites, groups, configs)
- ground-control/internal/database/: Database models and operations (PostgreSQL)
- ground-control/reg/harbor/: Harbor API client (projects, robots, replication)
- ground-control/migrator/: Database migration handling

### Key Concepts

Groups: Collections of container images that satellites replicate. Contains artifact metadata (repository, tag, digest, type).

Configs: Define how satellites connect to Ground Control, replication intervals, and local registry settings (including Zot config).

State Replication: Periodic sync where satellites fetch desired state from Ground Control and replicate artifacts locally.

Registration: Periodic heartbeat where satellites register with Ground Control using their token.

Mirror Configuration: Satellites configure container runtimes to use local registry as mirror, with fallback to upstream.

### Configuration Files

Satellite uses JSON configuration with three sections:
- state_config: Registry credentials and state URL
- app_config: Ground Control URL, log level, replication intervals, local registry settings
- zot_config: Embedded Zot registry configuration (storage, HTTP, logging)

Ground Control uses environment variables (see ground-control/.env.example):
- Harbor credentials (HARBOR_USERNAME, HARBOR_PASSWORD, HARBOR_URL)
- Database connection (DB_HOST, DB_PORT, DB_DATABASE, DB_USERNAME, DB_PASSWORD)
- Server settings (PORT, APP_ENV)

## Go Style Rules

- Use `any` instead of `interface{}`. The project targets Go 1.22+ where `any` is the preferred alias.
- Use `t.TempDir()` in tests instead of manual temp paths with `os.TempDir()`.
- Use `cm.With()` modifiers for all ConfigManager mutations (never mutate via `cm.GetConfig()` directly).

## Important Development Notes

### State Management

The satellite maintains state in a JSON file (default: config.json) containing current configuration, artifacts to replicate, and registry URLs/credentials. State is fetched from Ground Control at regular intervals (default: every 10 seconds, configurable via state_replication_interval).

### Container Runtime Integration

Satellite configures CRIs as mirrors using --mirrors flag:
- Format: --mirrors=<CRI>:<registry1>,<registry2>
- Example: --mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io
- Docker only supports mirroring docker.io, use --mirrors=docker:true
- Requires sudo for updating CRI config files
- Docker service restart required for changes to take effect

### Database Migrations

Ground Control uses SQL migrations in ground-control/migrator/. Migrations run automatically on startup.

### Hot Reload

Satellite supports hot-reloading configuration changes without restart via config file watching.

## Testing

- Unit tests colocated with source files (*_test.go)
- E2E tests in test/e2e/
- Test configs in test/e2e/testconfig/
- Use -args -abs=false for non-Dagger test runs

## CI/CD

GitHub Actions workflows:
- .github/workflows/test.yaml: vulnerability checks, test release, builds, E2E tests
- .github/workflows/lint.yaml: golangci-lint
- .github/workflows/release.yaml: GoReleaser

All CI uses Dagger for consistent builds.

## Architecture Decisions

ADRs in docs/decisions/:
- ADR-0001: Skopeo vs Crane (chose Skopeo for image copying)
- ADR-0002: Zot vs Docker Registry (chose Zot for OCI compliance)
- ADR-0003: Remote config injection (chose API-based config delivery)

## Common Workflows

### Adding a new API endpoint to Ground Control

1. Add handler in ground-control/internal/server/*_handlers.go
2. Register route in ground-control/internal/server/routes.go
3. Add database operations in ground-control/internal/database/ if needed
4. Update models in ground-control/internal/models/ if needed

### Adding a new satellite feature

1. Implement in appropriate internal/ package
2. Update pkg/config/ if configuration changes needed
3. Add validation in pkg/config/validate.go
4. Update cmd/main.go if new CLI flags needed
5. Update config.example.json

### Modifying state replication logic

State replication in internal/state/:
- fetcher.go: Fetching state from Ground Control
- replicator.go: Replicating artifacts to local registry
- state_process.go: Orchestrating the state sync process
- registration_process.go: Satellite registration with Ground Control

This is core functionality; changes require careful review.
