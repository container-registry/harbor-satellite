# Ground Control

Ground Control is the cloud-side management service for Harbor Satellite. It manages satellites, groups, configs, registration, desired state, status reporting, and Harbor integration.

Ground Control is part of the single Go module at the repository root. Its code lives under `internal/groundcontrol/` with the entrypoint in `cmd/groundcontrol/server/main.go`. Run all Go commands from the repository root.

## What It Starts

`cmd/groundcontrol/server/main.go` performs the Ground Control startup sequence:

- Checks Harbor health
- Runs PostgreSQL migrations
- Creates the HTTP server and routes
- Starts the cleanup job
- Serves HTTP, file-based TLS, or SPIFFE mTLS depending on configuration
- Shuts down gracefully on `SIGINT` or `SIGTERM`

## Common Commands

Populate the required environment variables first. For local development, use `.env.example` as the starting point for a `.env` file. Ground Control loads `.env` from the current working directory if it exists.

On first start against an empty database, Ground Control creates the `admin` system admin from `ADMIN_PASSWORD` and exits if the variable is unset or fails the password policy.

Run Ground Control locally from the repository root:

```bash
go run cmd/groundcontrol/server/main.go
```

Run Ground Control tests:

```bash
go test ./...
```

Start the local Docker Compose setup (PostgreSQL and Ground Control):

```bash
HARBOR_URL=https://harbor.example.com ADMIN_PASSWORD='<ADMIN_PASSWORD>' docker compose up -d
```

The root `docker-compose.yml` requires `HARBOR_URL` and passes `HARBOR_USERNAME`, `HARBOR_PASSWORD` (default `admin`/`Harbor12345`) and `ADMIN_PASSWORD` through from your shell or a root `.env`. Ground Control listens on `localhost:${GC_HOST_PORT:-8080}`, PostgreSQL on host port `8100`. The compose default `SKIP_HARBOR_HEALTH_CHECK=true` is overridden if your `.env` sets it (as `.env.example` does, to `false`). The `satellite` service is in the `satellite` profile and needs a registration token: `TOKEN=<token> docker compose up -d satellite`.

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
- Bootstrap: `ADMIN_PASSWORD` (required on first start)
- Server settings: `PORT` (default `8080`), `SESSION_DURATION` (default `24h`), `LOCKOUT_DURATION` (default `5m`), `STALE_THRESHOLD` (default `1h`)
- Harbor robot accounts: `ROBOT_DURATION_DAYS` (default `30`)
- Password policy: `PASSWORD_MIN_LENGTH`, `PASSWORD_MAX_LENGTH`, `PASSWORD_REQUIRE_UPPERCASE`, `PASSWORD_REQUIRE_LOWERCASE`, `PASSWORD_REQUIRE_NUMBER`, `PASSWORD_REQUIRE_SPECIAL`
- Optional TLS: `TLS_CERT_FILE`, `TLS_KEY_FILE`, `TLS_CA_FILE`
- Optional SPIFFE mTLS: `SPIFFE_SERVER_ENABLED`, `SPIFFE_TRUST_DOMAIN`, `SPIFFE_PROVIDER` (`sidecar` or `static`), `SPIFFE_ENDPOINT_SOCKET`, `SPIFFE_CERT_FILE`, `SPIFFE_KEY_FILE`, `SPIFFE_BUNDLE_FILE`
- Optional embedded SPIRE server: `EMBEDDED_SPIRE_ENABLED` plus the `SPIRE_*` variables
- Optional audit logging: `AUDIT_*` variables, see [Audit Logging](audit-logging.md)

The full list with defaults is in `internal/shared/env/ground-control.go`.

## Directory Guide

- `cmd/groundcontrol/server/main.go` - service entrypoint
- `cmd/groundcontrol/cli/root.go` - CLI entrypoint
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
- [Decision records](../decisions/README.md)
- [SPIFFE quickstarts](../../examples/deploy/spiffe/README.md)
