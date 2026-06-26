# Ground Control

Ground Control is the cloud-side management service for Harbor Satellite. It manages satellites, groups, configs, registration, desired state, status reporting, and Harbor integration.

This directory is a separate Go module with its own `go.mod`, dependencies, Docker setup, environment configuration, migrations, and generated database code. Run module-specific Go commands from this directory unless you are using a root `task` target that already changes into `ground-control/`.

## What It Starts

`main.go` performs the Ground Control startup sequence:

- Checks Harbor health
- Runs PostgreSQL migrations
- Creates the HTTP server and routes
- Starts the cleanup job
- Serves HTTP, file-based TLS, or SPIFFE mTLS depending on configuration
- Shuts down gracefully on `SIGINT` or `SIGTERM`

## Common Commands

Populate the required environment variables first. For local development, use `.env.example` as the starting point for a `.env` file.

Run Ground Control locally from this directory:

```bash
go run main.go
```

Run Ground Control tests:

```bash
go test ./...
```

Start the local Docker Compose setup:

```bash
docker compose up
```

Build both project components from the repository root:

```bash
task build
```

## Configuration

Ground Control reads environment variables directly, with `.env.example` documenting the common local settings.

Key groups include:

- Harbor access: `HARBOR_USERNAME`, `HARBOR_PASSWORD`, `HARBOR_URL`
- Local development: `SKIP_HARBOR_HEALTH_CHECK` (set to `true` when running without a Harbor instance)
- PostgreSQL access: `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD`
- Server settings: `PORT`
- Optional TLS: `TLS_CERT_FILE`, `TLS_KEY_FILE`, `TLS_CA_FILE`
- Optional SPIFFE/SPIRE settings
- Optional audit logging settings

## Directory Guide

- `main.go` - service entrypoint
- `internal/server` - routes, handlers, auth middleware, bootstrap, cleanup, and status APIs
- `internal/database` - sqlc-generated database access code
- `sql/schema` - PostgreSQL migrations
- `sql/queries` - sqlc query definitions
- `migrator` - migration runner
- `reg/harbor` - Harbor API client helpers
- `internal/spiffe` - SPIFFE/SPIRE provider and server client integration
- `internal/auth` - password policy and hashing helpers
- `pkg/crypto` - Ground Control crypto helpers

## Related Docs

- [Project README](../README.md)
- [Quickstart](../QUICKSTART.md)
- [Architecture docs](../docs/architecture/README.md)
- [SPIFFE quickstarts](../deploy/quickstart/spiffe/README.md)
