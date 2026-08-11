# Ground Control

Ground Control is the cloud-side management service for Harbor Satellite. It manages satellites, groups, configs, registration, desired state, status reporting, and Harbor integration.

Ground Control is part of the single Go module at the repository root. Its code lives under `internal/groundcontrol/` with the entrypoint in `cmd/ground-control/main.go`. Run all Go commands from the repository root.

## What It Starts

`cmd/ground-control/main.go` performs the Ground Control startup sequence:

- Checks Harbor health
- Runs PostgreSQL migrations
- Creates the HTTP server and routes
- Starts the cleanup job
- Serves HTTP, file-based TLS, or SPIFFE mTLS depending on configuration
- Shuts down gracefully on `SIGINT` or `SIGTERM`

## Common Commands

Populate the required environment variables first. For local development, use `.env.example` as the starting point for a `.env` file.

Run Ground Control locally from the repository root:

```bash
go run cmd/ground-control/main.go
```

Run Ground Control tests:

```bash
go test ./...
```

Start the local Docker Compose setup (PostgreSQL and Ground Control):

```bash
docker compose up postgres ground-control
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

- `cmd/ground-control/main.go` - service entrypoint
- `internal/groundcontrol/server` - routes, handlers, auth middleware, bootstrap, cleanup, and status APIs
- `internal/groundcontrol/database` - sqlc-generated database access code
- `internal/groundcontrol/sql/schema` - PostgreSQL migrations
- `internal/groundcontrol/sql/queries` - sqlc query definitions
- `internal/groundcontrol/migrator` - migration runner
- `internal/groundcontrol/harbor` - Harbor API client helpers
- `internal/groundcontrol/spiffe` - SPIFFE/SPIRE provider and server client integration
- `internal/groundcontrol/auth` - password policy and hashing helpers
- `internal/shared/crypto` - shared crypto helpers used by Ground Control

## Related Docs

- [Project README](../../README.md)
- [Quickstart](../../website/content/docs/quickstart.md)
- [Architecture docs](../architecture/README.md)
- [SPIFFE quickstarts](../../examples/deploy/spiffe/README.md)
