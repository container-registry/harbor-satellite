# Ground Control

Ground Control is the cloud-side management service for Harbor Satellite. It manages satellites, groups, configs, registration, desired state, status reporting, and Harbor integration.

Ground Control is part of the single Go module at the repository root. Its code lives under `internal/groundcontrol/` with the entrypoint in `cmd/groundcontrol/main.go`. Run all Go commands from the repository root.

## What It Starts

`groundcontrol serve` performs the Ground Control startup sequence:

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
go run ./cmd/groundcontrol serve
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
- PostgreSQL access: `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD` (escaped in the connection URL), `DB_SSLMODE` (default `require`; one of `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`) and `DB_SSLROOTCERT` (CA file for `verify-ca`/`verify-full`). A server without TLS needs `DB_SSLMODE=disable`
- Connection pools, one per plane: API (operator routes and migrations), fleet (satellite routes: heartbeat, credential refresh, satellite authentication) and async (background jobs). `DB_URL_API`, `DB_URL_FLEET` and `DB_URL_ASYNC` give a plane its own URL; without one, fleet and async share the API pool unless one of their pool settings is set. Pool settings per plane are `DB_<PLANE>_MAX_OPEN_CONNS`, `_MAX_IDLE_CONNS`, `_CONN_MAX_LIFETIME`, `_CONN_MAX_IDLE_TIME`, `_STATEMENT_TIMEOUT`, `_LOCK_TIMEOUT` and `_IDLE_IN_TX_SESSION_TIMEOUT`, with `<PLANE>` one of `API`, `FLEET`, `ASYNC`. The timeouts are sent as connection parameters, and every connection carries `application_name` `groundcontrol-api`, `groundcontrol-fleet` or `groundcontrol-async`, which `pg_stat_activity` shows
- Bootstrap: `ADMIN_PASSWORD` (required on first start)
- Server settings: `PORT` (default `8080`), `SESSION_DURATION` (default `24h`), `LOCKOUT_DURATION` (default `5m`), `STALE_THRESHOLD` (default `15m`, not read yet)
- Failed authentication limit per client IP (login and Basic auth, any username): `AUTH_FAILURE_LIMIT` (default `20`) failures within `AUTH_FAILURE_WINDOW` (default `1m`) before 429, `0` disables it. Independently, 5 failed attempts for one username, through `/login` or Basic auth, lock that account for `LOCKOUT_DURATION`
- Fleet hot path (plan item 4, ADR-0016; parsed but not used yet, except that `RATE_LIMIT_SPLIT=true` builds the split limiters, which answer 501 until 4.6, so leave it off): `SATELLITE_SESSION_TTL` (`720h`), `SATELLITE_SESSION_CACHE_TTL` (`30s`, at most `5m`), `SATELLITE_SESSION_CACHE_SIZE` (`10000`), `SATELLITE_ROBOT_AUTH` (`true`), `HEARTBEAT_FLUSH_INTERVAL` (`5s`), `HEARTBEAT_BUFFER_MAX` (`10000`), `HEARTBEAT_MAX_FLUSH_FAILURES` (`3`), `STALE_CHECK_INTERVAL` (`1m`), `SESSION_EXPIRY_INTERVAL` (`1h`), `STATUS_RETENTION_DAYS` (`7`), `STATUS_PARTITIONS_AHEAD` (`3`), `RATE_LIMIT_SPLIT` (`false`), `RATE_LIMIT_ANONYMOUS` (`10/1m`), `RATE_LIMIT_FLEET` (`3/1s`)
- Harbor robot accounts: `ROBOT_DURATION_DAYS` (default `30`)
- Password policy: `PASSWORD_MIN_LENGTH`, `PASSWORD_MAX_LENGTH`, `PASSWORD_REQUIRE_UPPERCASE`, `PASSWORD_REQUIRE_LOWERCASE`, `PASSWORD_REQUIRE_NUMBER`, `PASSWORD_REQUIRE_SPECIAL`
- Optional TLS: `TLS_CERT_FILE`, `TLS_KEY_FILE`, `TLS_CA_FILE`
- Optional SPIFFE mTLS: `SPIFFE_SERVER_ENABLED`, `SPIFFE_TRUST_DOMAIN`, `SPIFFE_PROVIDER` (`sidecar` or `static`), `SPIFFE_ENDPOINT_SOCKET`, `SPIFFE_CERT_FILE`, `SPIFFE_KEY_FILE`, `SPIFFE_BUNDLE_FILE`
- Optional embedded SPIRE server: `EMBEDDED_SPIRE_ENABLED` plus the `SPIRE_*` variables
- Optional audit logging: `AUDIT_*` variables, see [Audit Logging](audit-logging.md)

The full list with defaults is in `internal/shared/env/groundcontrol.go`.

## Directory Guide

- `cmd/groundcontrol/main.go` - signal-aware executable entrypoint
- `internal/groundcontrol/cli/root/serve.go` - server command and lifecycle
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

- [Command migration](groundcontrol-command-migration.md)
- [Project README](../../README.md)
- [Quickstart](../../website/content/docs/quickstart.md)
- [Decision records](../decisions/README.md)
- [SPIFFE quickstarts](../../examples/deploy/spiffe/README.md)

## Database roles per plane

Where the deployment can create PostgreSQL roles, give each plane its own role and URL, so the server enforces the connection budget and the timeouts even if Ground Control's own settings are wrong:

```sql
CREATE ROLE gc_api   LOGIN PASSWORD '...' CONNECTION LIMIT 20;
CREATE ROLE gc_fleet LOGIN PASSWORD '...' CONNECTION LIMIT 40;
CREATE ROLE gc_async LOGIN PASSWORD '...' CONNECTION LIMIT 10;
GRANT gc_owner TO gc_api, gc_fleet, gc_async;   -- the role that owns the schema

ALTER ROLE gc_fleet SET statement_timeout = '5s';
ALTER ROLE gc_fleet SET lock_timeout = '1s';
ALTER ROLE gc_fleet SET idle_in_transaction_session_timeout = '10s';
ALTER ROLE gc_async SET statement_timeout = '5min';
```

Then set `DB_URL_API`, `DB_URL_FLEET` and `DB_URL_ASYNC` to URLs with those roles, and keep `DB_<PLANE>_MAX_OPEN_CONNS` below each `CONNECTION LIMIT`. Migrations run with the API URL, so the API role needs the DDL privileges. A managed service that allows one role only works with the single DSN and the `DB_<PLANE>_*` settings.

## Upgrading

### Migration 023: constraints and time zones

Migration 023 adds `UNIQUE (satellite_id)` to `robot_accounts`, makes deleting a config that satellites use fail (`ON DELETE RESTRICT`), checks `satellites.heartbeat_interval` against `@every HHhMMmSSs`, adds `configs.version` (a concurrent `PATCH /api/configs/{config}` now answers 409 instead of overwriting the other update), and turns every `TIMESTAMP` column into `TIMESTAMPTZ`.

**Duplicate robot accounts.** The migration stops with `robot_accounts has several robots for satellites: ...` when a satellite has more than one row in `robot_accounts`. Keep the newest row per satellite and delete the others:

```sql
SELECT satellite_id, id, robot_name, robot_id, created_at
FROM robot_accounts
WHERE satellite_id IN (SELECT satellite_id FROM robot_accounts GROUP BY satellite_id HAVING count(*) > 1)
ORDER BY satellite_id, created_at DESC, id DESC;

DELETE FROM robot_accounts r
USING robot_accounts newer
WHERE newer.satellite_id = r.satellite_id
  AND (newer.created_at, newer.id) > (r.created_at, r.id);
```

Delete the Harbor robots of the removed rows (`robot_id`) in Harbor as well, then start Ground Control again.

**Time zones.** The conversion reads every stored value as UTC wall-clock time (`AT TIME ZONE 'UTC'`). Values written with `NOW()` follow the database's `TimeZone` setting, and values written by Ground Control (for example `robot_expiry`, `expires_at`, `locked_until`) follow the time zone of the Ground Control process. Check that both were UTC before upgrading; otherwise adjust the stored values first.
