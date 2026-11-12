# Ground Control

Ground Control is the cloud-side management service for Harbor Satellite. It manages satellites, groups, configs, registration, desired state, status reporting and the Harbor robot accounts of the fleet.

Ground Control is part of the single Go module at the repository root. Its code lives under `internal/groundcontrol/` with the entrypoint in `cmd/groundcontrol/main.go`. One executable, `groundcontrol`, is both the server (`groundcontrol serve`) and the administration client.

## What `serve` Starts

1. Loads the environment, including a `.env` file in the working directory.
2. Checks Harbor and only logs the result: Ground Control starts while Harbor is unreachable, and `/health` reports it `degraded`.
3. Runs the PostgreSQL migrations.
4. Opens one connection pool per plane (API, fleet, async) and composes the fleet services.
5. Creates the `admin` system administrator from `ADMIN_PASSWORD` on an empty database. It exits when the variable is unset or fails the password policy.
6. Starts the background jobs: the cleanup job, the Harbor probe, the heartbeat flusher, the policy poller with `POLICY_ENGINE=casbin`, the rotation cache with `ROBOT_ROTATION_ENABLED=true`, and the async worker unless `--worker=off`.
7. Serves HTTP, TLS or SPIFFE mTLS, and shuts down gracefully on `SIGINT` or `SIGTERM`.

`groundcontrol worker` runs the async worker alone, for deployments that scale it separately from the API.

## Common Commands

Run Ground Control locally from the repository root, with a `.env` copied from `.env.example`:

```bash
go run ./cmd/groundcontrol serve
```

Start PostgreSQL and Ground Control with Docker Compose:

```bash
HARBOR_URL=https://harbor.example.com ADMIN_PASSWORD='<ADMIN_PASSWORD>' docker compose up -d
```

The root `docker-compose.yml` requires `HARBOR_URL` and passes `HARBOR_USERNAME`, `HARBOR_PASSWORD` (default `admin`/`Harbor12345`) and `ADMIN_PASSWORD` through from your shell or a root `.env`. It sets `DB_SSLMODE=disable` for its PostgreSQL, which serves no TLS. Ground Control listens on `localhost:${GC_HOST_PORT:-8080}`, PostgreSQL on host port `8100`. The `satellite` service is in the `satellite` profile and needs a registration token: `TOKEN=<token> docker compose up -d satellite`.

Build both components:

```bash
task build
```

Run the tests, and the integration tests against a disposable PostgreSQL:

```bash
go test ./...
task test:integration
```

## Configuration

Every environment variable, with its default, is in the [Ground Control Configuration Reference](../reference/ground-control.md). The API is in the [API Reference](../reference/api.md). Operations topics:

- [Database Operations](../operations/database.md): PostgreSQL versions, TLS, pools and roles per plane, retention
- [Upgrading](../operations/upgrading.md), including migration 023
- [Backup and Restore](../operations/backup-and-restore.md)
- [TLS](../operations/tls.md)
- [Monitoring](../operations/monitoring.md)
- [Troubleshooting](../operations/troubleshooting.md)

## Directory Guide

- `cmd/groundcontrol/main.go`: signal-aware entrypoint
- `internal/groundcontrol/cli/root/`: `serve`, `worker` and the client commands
- `internal/groundcontrol/app/`: composition root (`NewFleet`, `NewWorker`)
- `internal/groundcontrol/server/`: generated router, handlers, authentication and route security
- `internal/groundcontrol/fleet/`: domain types, services and ports
- `internal/groundcontrol/adapters/`: `postgres`, `harbor2`, `spire`, `casbin`, `oidc`
- `internal/groundcontrol/async/`: outbox publisher, reconcilers and periodic jobs
- `internal/groundcontrol/authn/`, `heartbeat/`, `webhook/`, `secrets/`, `satconfig/`, `dbpool/`
- `internal/groundcontrol/sql/schema/` and `sql/queries/`: migrations and sqlc queries; `internal/groundcontrol/database/` is generated
- `internal/groundcontrol/spiffe/`: SPIFFE provider, embedded SPIRE server and SPIRE client

## Related Docs

- [Architecture](../architecture.md)
- [Command migration](groundcontrol-command-migration.md)
- [Token quickstart](../../examples/deploy/no-spiffe/quickstart.md) and [SPIFFE quickstarts](../../examples/deploy/spiffe/README.md)
- [Decision records](../decisions/README.md)
