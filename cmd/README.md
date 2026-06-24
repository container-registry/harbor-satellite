# Satellite Command

This directory contains the main entrypoint for the Harbor Satellite edge process.

`cmd/main.go` reads CLI flags and environment variables, initializes runtime configuration, starts the local registry path, and launches the satellite schedulers that register with Ground Control, replicate state, and report status.

## What It Starts

- Configuration loading and validation through `pkg/config`
- Optional SPIFFE/SPIRE and PARSEC setup
- Embedded Zot registry setup, or BYO registry configuration
- Optional container runtime fallback/mirror configuration
- Config file watching and hot reload
- Satellite registration, state replication, and heartbeat schedulers
- Graceful shutdown for running scheduler and registry work

## Common Commands

Build the satellite from the repository root:

```bash
task _build:satellite
```

Run the satellite directly:

```bash
go run cmd/main.go \
  --token "<token>" \
  --ground-control-url "http://127.0.0.1:8080" \
  --harbor-registry-url "https://harbor.example.com"
```

Run root-module tests:

```bash
go test ./...
```

## Important Options

Most options can be provided as CLI flags or environment variables.

| CLI Flag | Env Var | Purpose |
|---|---|---|
| `--token` | `TOKEN` | Token used for token-based registration |
| `--ground-control-url` | `GROUND_CONTROL_URL` | Ground Control API URL |
| `--harbor-registry-url` | `HARBOR_REGISTRY_URL` | Harbor registry URL override |
| `--config-dir` | `CONFIG_DIR` | Satellite config directory |
| `--registry-data-dir` | `REGISTRY_DATA_DIR` | Zot registry data directory |
| `--byo-registry` | `BYO_REGISTRY` | Use an external registry instead of embedded Zot |
| `--registry-url` | `REGISTRY_URL` | External registry URL for BYO mode |
| `--spiffe-enabled` | `SPIFFE_ENABLED` | Enable SPIFFE/SPIRE authentication |
| `--parsec-enabled` | `PARSEC_ENABLED` | Enable PARSEC hardware-backed identity in parsec-tagged builds |
| `--parsec-socket` | `PARSEC_SOCKET` | PARSEC daemon socket path |

## Related Code

- `pkg/config` - configuration model, defaults, validation, and persistence
- `internal/satellite` - satellite scheduler orchestration
- `internal/state` - registration, state fetching, replication, and reporting
- `internal/registry` - embedded Zot registry management
- `internal/container_runtime` - container runtime fallback and mirror configuration
- `internal/hotreload` and `internal/watcher` - config reload handling

## Related Docs

- [Project README](../README.md)
- [Quickstart](../QUICKSTART.md)
- [Architecture docs](../docs/architecture/README.md)
