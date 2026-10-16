# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Harbor Satellite is a registry fleet management and artifact distribution solution that extends Harbor container registry to edge computing environments. Two main components:

1. Satellite: Runs at edge locations, replicates OCI content from Harbor into a local ORAS OCI image layout (default) or an external BYO registry, and serves pulls to local workloads through a loopback OCI proxy
2. Ground Control (GC): Cloud-side management service for device management, onboarding, state management, and artifact orchestration. The `groundcontrol` executable provides administration commands and starts the server with `serve`

## Build and Development Commands

### Building

```bash
# Build Satellite and Ground Control (Taskfile)
task build

# Build individual components
task _build:satellite
task _build:groundcontrol

# Run satellite directly
go run ./cmd/satellite --token "<token>" --ground-control-url "<url>" --harbor-registry-url "<harbor-url>"

# Run Ground Control directly
go run ./cmd/groundcontrol serve
```

### Code Generation

```bash
# OpenAPI server (internal/groundcontrol/server/server.gen.go) and client (pkg/groundcontrol/client.gen.go)
task generate:groundcontrol     # alias gen:groundcontrol; also gen:groundcontrol-server, gen:groundcontrol-client

# sqlc: queries in internal/groundcontrol/sql/queries, schema in internal/groundcontrol/sql/schema
sqlc generate                    # config in sqlc.yaml, output in internal/groundcontrol/database/
```

The spec is `spec/groundcontrol/openapi.yaml` (oapi-codegen configs in `spec/groundcontrol/oapi-codegen/`). Route matching, parameter binding and request/response types come from the generated server; do not hand-edit `*.gen.go` or `internal/groundcontrol/database/*.sql.go`.

### Testing

```bash
# Run all tests
go test ./... -v -count=1

# Run a single test
go test -v -run TestFunctionName ./path/to/package

# Ground Control tests against a disposable Postgres (also runs the unit tests of internal/groundcontrol)
task test:integration

# Run E2E tests (main, BYO, SPIFFE; crash-recovery variants run separately)
task e2e

# Run individual E2E variants
task e2e-test               # Standard E2E (local OCI layout)
task e2e-byo                # BYO registry E2E
task e2e-spiffe             # SPIFFE join-token E2E
task e2e-crash-recovery     # Crash recovery with the local OCI store
task e2e-crash-recovery-byo # Crash recovery with BYO registry
task e2e-multi-group        # Satellite in two groups (pending plan item D5, runs with HARBOR3_PENDING=1)

# Fleet load test (build tag load) against a running GC: LOAD_GC_URL, LOAD_ADMIN_PASSWORD
task test:load

# Harbor-backed variants against a running Harbor instead of the bundled one: containers, projects,
# robots and state artifacts are prefixed hs-e2e-, GC listens on host port 9180, cleanup removes only those
HARBOR_EXTERNAL=1 HARBOR_NETWORK=<harbor network> HARBOR_HOST_URL=http://localhost:<port> task e2e-test
```

### Linting

```bash
task lint          # task lint:arch, then golangci-lint in a pinned Docker image
task lint:arch     # import boundaries in .go-arch-lint.yml (go-arch-lint via go run)
task lint-fix
task lint-report   # writes golangci-lint.report
task vuln          # govulncheck in a pinned Docker image
```

Single config `.golangci.yaml` (golangci-lint v2, CI pins v2.12.2) with `default: none`, 51 enabled linters and the gofmt, gofumpt and goimports formatters. Notable enabled linters: gosec, errcheck (check-blank, type assertions), errorlint, depguard (bans io/ioutil), govet (all analyzers except fieldalignment and shadow), interfacebloat (max 10 methods), nakedret (no naked returns at all), tagalign, nolintlint (requires explanation). `cyclop`, `funlen`, `wrapcheck` and others have settings in the file but are not enabled; `gochecknoglobals` and `gochecknoinits` are not enabled either.

### Running Locally

```bash
# Dev stack built from source: PostgreSQL (host port 8100) + Ground Control (localhost:${GC_HOST_PORT:-8080}).
# HARBOR_URL is required; values can also come from a root .env
HARBOR_URL=<harbor-url> ADMIN_PASSWORD=<password> docker compose up -d
# Satellite is in the "satellite" profile; HARBOR_REGISTRY_URL defaults to HARBOR_URL
TOKEN=<token> docker compose up -d satellite
docker compose --profile satellite down

# Satellite with a registry:2 sidecar in BYO mode
task byo-up    # docker-compose.byo.yml; task byo-down to stop

# Satellite with Go
go run ./cmd/satellite --token "<token>" --ground-control-url "http://127.0.0.1:8080" --harbor-registry-url "<harbor-url>"

# Satellite with CRI mirror config pointing at the local OCI proxy (see Container Runtime Integration)
go run ./cmd/satellite --token "<token>" --ground-control-url "<url>" --harbor-registry-url "<harbor-url>" \
  --mirrors=containerd:docker.io,quay.io

# Ground Control with Docker Compose
docker compose up postgres ground-control

# Ground Control with Go (requires .env file)
go run ./cmd/groundcontrol serve
```

## Architecture

### Module Structure

One Go module at the repository root (`github.com/container-registry/harbor-satellite`, `go 1.26.5`). It builds two binaries, `satellite` and `groundcontrol`. Ground Control administration commands and the `serve` subcommand share one executable. Keep entrypoints in `cmd/` and implementation under `internal/`; `pkg/` holds the satellite config package and the generated GC client.

- cmd/satellite/: satellite entrypoint
- cmd/groundcontrol/: Signal-aware `groundcontrol` entrypoint (Cobra)

### Satellite Component Structure

- cmd/satellite/main.go: CLI flags and env vars, store setup, OCI proxy listener, CRI config application, direct-delivery setup, audit logger, config watcher, graceful shutdown
- pkg/config/: Configuration management (ConfigManager), validation and default enforcement, path resolution, optional encryption on write
- internal/satellite/satellite.go: Orchestration: picks token or SPIFFE ZTR, starts ZTR, state replication and status report schedulers, registers events
- internal/satellite/state/: State fetching, replication, token and SPIFFE registration, status reporting, state persistence, direct delivery
- internal/satellite/store/: `Store` interface (Pull, Fetch, Replicate, Delete); `OCIStore` (ORAS OCI image layout, default local store) and `RegistryStore` (ORAS remote repository, used for Harbor as the source and for a BYO registry as the local store)
- internal/satellite/proxy/: Pull-only OCI Distribution proxy (ADR-0009): request parser and validation, handler chain, `proxy`/`replica` modes; `proxy/process/image/` is the pull handler
- internal/satellite/peers/: `EligiblePeers` builds the peer allow-list from `peer_distribution` (merges static_peers and gc_peers, applies the group filter). No caller outside tests yet; nothing dials peers
- internal/satellite/events/: Event scheduler for GC-triggered one-shot jobs (currently only `refresh_credentials`)
- internal/satellite/scheduler/: Interval scheduler (`@every <duration>` parsed into a time.Ticker, runs once immediately on start)
- internal/satellite/process/: `Process` interface (Name, Execute, IsRunning, IsComplete)
- internal/satellite/container_runtime/: CRI config management (containerd, CRI-O, Docker, Podman) and k3s/RKE2 image dir detection
- internal/satellite/hotreload/, internal/satellite/watcher/: Config file watching and hot reload (log level, state replication interval; audit config is swapped in cmd/satellite)
- internal/satellite/identity/: Device fingerprint (machine-id, MAC, disk serial); real only on Linux default builds
- internal/satellite/secure/: Device-bound config encryption (AES-256-GCM, Argon2id key from fingerprint)
- internal/satellite/tls/: Client TLS config loading

### Shared Packages

- internal/shared/env/: All env var parsing for satellite and GC (caarlos0/env struct tags)
- internal/shared/logger/: zerolog logger (context-carried) and audit logger (syslog daemon/network/file, OTLP/HTTP)
- internal/shared/crypto/: AES-GCM provider, Argon2id helpers (m=19 MiB, t=2, p=1)
- internal/shared/spiffe/: SPIFFE Workload API client (X.509 SVID, mTLS HTTP client)
- internal/shared/utils/: Misc utilities (signal context, URL formatting, file I/O)
- internal/version/: Version and GitCommit, set via ldflags

### Ground Control Component Structure

- cmd/groundcontrol/main.go: Creates the signal-aware context and executes the unified command
- internal/groundcontrol/cli/root/serve.go: Loads env, checks Harbor health, runs migrations, starts HTTP/TLS/SPIFFE server and the cleanup job, and shuts down on context cancellation
- internal/groundcontrol/server/: Handlers (satellites, groups, configs, auth, users, SPIRE), generated router (`server.gen.go`), per-route security middleware (`routes.go`), cleanup job
- internal/groundcontrol/database/: sqlc-generated PostgreSQL access (do not edit)
- internal/groundcontrol/sql/: `schema/` (goose migrations, also the sqlc schema) and `queries/`
- internal/groundcontrol/migrator/: goose runner; reads `/migrations` when present, otherwise uses the embedded SQL schema so installed binaries work outside the checkout
- internal/groundcontrol/harbor/: Harbor v2 API client (projects, robots, replication)
- internal/groundcontrol/harborhealth/: Startup Harbor health check (skippable via SKIP_HARBOR_HEALTH_CHECK)
- internal/groundcontrol/auth/: Password policy validation, Argon2id password hashing (via internal/shared/crypto), session token generation
- internal/groundcontrol/middleware/: Rate limiting, TLS cert watching
- internal/groundcontrol/spiffe/: SPIFFE provider, middleware, authorizer, embedded SPIRE server, SPIRE server client
- internal/groundcontrol/cli/: `groundcontrol` commands (serve, auth, get, create, update, delete, register, add, remove, sync, ping, health)
- internal/groundcontrol/utils/: State/config artifact URL helpers, robot project updates
- internal/groundcontrol/logger/: Unused copy of internal/shared/logger audit code
- pkg/groundcontrol/: Generated Go client for the GC API

### Key Concepts

Groups: Collections of artifacts (repository, tag, digest, type) that satellites replicate. GC pushes each group's state as an OCI artifact into Harbor under `satellite/group-state/<group>/state`.

Configs: Named satellite app configs (intervals, metrics, BYO registry, registry fallback, audit, TLS, peer distribution, etc.) delivered through Harbor as config state artifacts (`satellite/config-state/<config>/state`). Updates use JSON Merge Patch (evanphx/json-patch).

State Replication: The satellite fetches its root state artifact (`satellite/satellite-state/<name>/state`) directly from Harbor with its robot credentials, then fetches each group state and replicates changes into the local store. GC is not on the replication data path.

OCI Proxy: Unless `--fallback-only`, cmd/satellite always serves a pull-only OCI Distribution endpoint on `127.0.0.1:<proxy-port>` (default 8585, loopback hard-coded) backed by the same local store. `proxy` mode (default) fills a local miss from Harbor (GET replicates into the local store, HEAD only reads metadata); `replica` mode serves only replicated content. Push, delete, tag list, referrers and catalog return UNSUPPORTED. CEL policy and a `cache` mode from ADR-0009 are not implemented. `cmd/satellite` wraps the pull handler with `proxy/middleware.Stack(nil, nil, nil)`: the edge chain (metrics, authentication, admission) of ADR-0019 has empty slots.

Registration (ZTR, Zero Touch Registration): Two mutually exclusive paths:
- Token ZTR: Satellite POSTs `{"token": ...}` to `/satellites/ztr` and receives Harbor robot credentials and its state URL. Tokens expire after 24h and are deleted after a successful ZTR. Managed in state/registration_process.go.
- SPIFFE ZTR: Satellite presents an X.509 SVID via mTLS to `GET /satellites/spiffe-ztr` and is auto-registered. Managed in state/spiffe_registration.go.

Heartbeat and Status Reporting: The satellite POSTs to `/satellites/sync` (SPIFFE identity or robot Basic auth) with CPU, memory, storage, image count, cached images (BYO only) and CRI activity. `last_sync_duration_ms`, `latest_state_digest` and `latest_config_digest` are in the payload but never populated. The response can carry events (e.g. `refresh_credentials` when the robot expires within two heartbeat intervals). Managed in state/reporting_process.go and state/report.go.

Stale detection: A satellite is stale when `last_seen` is older than 3x its reported heartbeat interval (15 min if not `@every`). `STALE_THRESHOLD` is parsed but not used by any query.

BYO Registry: `--byo-registry --registry-url ...` replicates into an external registry instead of the local OCI layout. The proxy then serves from that registry, and it is the only endpoint `--fallback-only` can point CRIs at.

Peer Distribution (ADR-0010, proposed): opt-in `app_config.peer_distribution` block (static_peers, gc_peers, local_groups, reachout_sats group|global, timeout, retries, concurrency) is validated, defaulted when present, and kept across remote config reconciles. No peer copy is implemented.

Direct Delivery (experimental): `--direct-delivery` writes docker-save tarballs pulled from Harbor into the k3s/RKE2 agent images directory.

### Configuration Files

Satellite config directory defaults to `os.UserConfigDir()/satellite` (`~/.config/satellite` on Linux, `~/Library/Application Support/satellite` on macOS), overridable via `--config-dir` / `CONFIG_DIR`. It contains `config.json`, `prev_config.json`, `state.json` and the OCI layout in `oci/` (override with `--registry-data-dir` / `REGISTRY_DATA_DIR`).

`config.json` has two sections:
- state_config: Harbor robot credentials (`auth`) and the satellite state URL (`state`)
- app_config: Ground Control URL, log level, intervals, metrics, BYO registry (`bring_own_registry`, `local_registry`), TLS, SPIFFE, `encrypt_config`, `registry_fallback`, `harbor_registry_url`, `direct_delivery`, `audit`, `peer_distribution`

Example: `examples/config.example.json`. Fallback intervals when missing or invalid: state replication `@every 30s`, registration `@every 5s`, heartbeat `@every 30s` (pkg/config/constants.go).

Config encryption is opt-in via `app_config.encrypt_config` (default false) and is read once when the ConfigManager is created.

### Environment Variables

Both binaries load `.env` from the working directory if present. Template: `.env.example`. Source of truth: `internal/shared/env/`.

Satellite (each also has a CLI flag, see Satellite CLI Flags): TOKEN, GROUND_CONTROL_URL, HARBOR_REGISTRY_URL, USE_UNSECURE, SPIFFE_ENABLED, SPIFFE_ENDPOINT_SOCKET, SPIFFE_EXPECTED_SERVER_ID, BYO_REGISTRY, REGISTRY_URL, REGISTRY_USERNAME, REGISTRY_PASSWORD, CONFIG_DIR, REGISTRY_DATA_DIR, SHUTDOWN_TIMEOUT (30s), NO_REGISTRY_FALLBACK, DIRECT_DELIVERY, IMAGE_DIR, PROXY_MODE (proxy|replica, default proxy), PROXY_PORT (8585). Parsed but not used yet (Harbor 3 skeleton, no flag): TELEMETRY_ADDR, PROXY_MAX_CONNECTIONS (0), PROXY_WRITE_TIMEOUT (0s), PROXY_STREAMING (false), PROXY_PREFETCH_CONCURRENCY (2).

Ground Control:
- Harbor: HARBOR_URL, HARBOR_USERNAME, HARBOR_PASSWORD, ROBOT_DURATION_DAYS (30), SKIP_HARBOR_HEALTH_CHECK (false, testing only). Parsed but not used yet (Harbor 3 skeleton, plan item 2): HARBOR_RATE_LIMIT (20 requests/s toward Harbor), HARBOR_PROBE_INTERVAL (30s)
- Outbox publisher (Harbor 3 skeleton, parsed but not used): OUTBOX_BATCH_SIZE (100), OUTBOX_POLL_INTERVAL (1s), OUTBOX_CONCURRENCY (4), OUTBOX_MAX_ATTEMPTS (10), OUTBOX_BACKOFF_BASE (1s), OUTBOX_BACKOFF_MAX (5m), OUTBOX_LEASE (1m), OUTBOX_RETENTION (168h, plan item 9.1)
- Database: DB_HOST, DB_PORT, DB_DATABASE, DB_USERNAME, DB_PASSWORD (sslmode=disable). Parsed but not used yet (Harbor 3 skeleton): DB_SSLMODE (disable), DB_SSLROOTCERT, DB_URL_API, DB_URL_FLEET, DB_URL_ASYNC, and per plane DB_API_*, DB_FLEET_*, DB_ASYNC_* with MAX_OPEN_CONNS, MAX_IDLE_CONNS, CONN_MAX_LIFETIME, CONN_MAX_IDLE_TIME, STATEMENT_TIMEOUT, LOCK_TIMEOUT, IDLE_IN_TX_SESSION_TIMEOUT
- Server and auth: PORT (8080), ADMIN_PASSWORD (bootstraps `admin`), SESSION_DURATION (24h), LOCKOUT_DURATION (5m), STALE_THRESHOLD (15m, not read yet), AUTH_FAILURE_LIMIT (20) and AUTH_FAILURE_WINDOW (1m) (D6 skeleton, failed authentications per client IP; not enforced yet), SYNC_MAX_BODY_BYTES (4 MiB, plan item 6.5 skeleton, not enforced yet)
- Fleet hot path (plan item 4 skeleton, parsed but not used; RATE_LIMIT_SPLIT=true builds the split limiters, which answer 501 until 4.6, so leave it off): SATELLITE_SESSION_TTL (720h), SATELLITE_SESSION_CACHE_TTL (30s, at most 5m), SATELLITE_SESSION_CACHE_SIZE (10000), SATELLITE_ROBOT_AUTH (true), HEARTBEAT_FLUSH_INTERVAL (5s), HEARTBEAT_BUFFER_MAX (10000), HEARTBEAT_MAX_FLUSH_FAILURES (3), STALE_CHECK_INTERVAL (1m), SESSION_EXPIRY_INTERVAL (1h), STATUS_RETENTION_DAYS (7), STATUS_PARTITIONS_AHEAD (3), RATE_LIMIT_SPLIT (false), RATE_LIMIT_ANONYMOUS (10/1m), RATE_LIMIT_FLEET (3/1s)
- Policy (plan item 7 skeleton): POLICY_ENGINE (builtin|casbin, default builtin; any other value fails startup, and so does casbin without a policy passed with WithPolicy, which serve does not do yet), POLICY_POLL_INTERVAL (10s)
- Identity (plan item 8 skeleton, parsed but not used): OIDC_ENABLED (false), OIDC_ISSUER_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, OIDC_REDIRECT_URL, OIDC_SCOPES (openid,profile,email,groups), OIDC_USERNAME_CLAIM (preferred_username), OIDC_GROUPS_CLAIM (groups), OIDC_ROLE_MAP (group=role,...), OIDC_DISABLE_PASSWORD_LOGIN (false), OIDC_LOGIN_STATE_TTL (10m), ROBOT_ROTATION_ENABLED (false), ROBOT_ROTATION_BEFORE_EXPIRY (72h), ROBOT_ROTATION_INTERVAL (10m), ROBOT_ROTATION_CACHE_REFRESH (30s), DATA_ENCRYPTION_KEY (base64, 32 bytes)
- Webhooks (plan item 9 skeleton, parsed but not used): WEBHOOKS_ENABLED (false), WEBHOOK_BATCH_SIZE (100), WEBHOOK_POLL_INTERVAL (1s), WEBHOOK_BACKOFF_BASE (1s), WEBHOOK_BACKOFF_MAX (5m), WEBHOOK_TIMEOUT (10s), WEBHOOK_DISABLE_AFTER (24h), WEBHOOK_SOURCE (urn:harbor-satellite:ground-control)
- Password policy: PASSWORD_MIN_LENGTH (8), PASSWORD_MAX_LENGTH (128), PASSWORD_REQUIRE_UPPERCASE/LOWERCASE/NUMBER (true), PASSWORD_REQUIRE_SPECIAL (false)
- TLS: TLS_CERT_FILE, TLS_KEY_FILE (both set enables HTTPS), TLS_CA_FILE (enables client cert verification)
- SPIFFE: SPIFFE_SERVER_ENABLED (note: not SPIFFE_ENABLED, that one is the satellite's), SPIFFE_TRUST_DOMAIN (harbor-satellite.local), SPIFFE_PROVIDER (sidecar|static), SPIFFE_ENDPOINT_SOCKET, SPIFFE_CERT_FILE, SPIFFE_KEY_FILE, SPIFFE_BUNDLE_FILE
- SPIRE: EMBEDDED_SPIRE_ENABLED, SPIRE_DATA_DIR, SPIRE_TRUST_DOMAIN, SPIRE_BIND_ADDRESS, SPIRE_SERVER_SOCKET, SPIRE_SERVER_ADDRESS, SPIRE_SERVER_PORT (8081)
- Audit: AUDIT_LOG_ENABLED (false), AUDIT_SYSLOG_ENABLED (true), AUDIT_SYSLOG_TARGET (daemon|network|file, default file), AUDIT_SYSLOG_TAG, AUDIT_SYSLOG_SOCKET_PATH, AUDIT_SYSLOG_NETWORK, AUDIT_SYSLOG_ADDRESS, AUDIT_SYSLOG_FILE_PATH, AUDIT_SYSLOG_FILE_MAX_SIZE_MB/MAX_BACKUPS/MAX_AGE_DAYS/COMPRESS, AUDIT_OTEL_ENDPOINT, AUDIT_TRUST_FORWARDED_HEADERS

Ground Control CLI (`groundcontrol`): flags `--server` (default https://localhost:8080/), `--token`, `--timeout`, `--insecure`, `--config`; env prefix `GROUND_CONTROL_` (GROUND_CONTROL_URL, GROUND_CONTROL_TOKEN). Passwords only via env: GROUND_CONTROL_PASSWORD, GROUND_CONTROL_USER_PASSWORD, GROUND_CONTROL_CURRENT_PASSWORD, GROUND_CONTROL_NEW_PASSWORD.

### Authentication

**Ground Control API Authentication**: Session-based auth with two methods:
- Bearer Token: `Authorization: Bearer <session_token>` (from /login)
- Basic Auth: `Authorization: Basic <base64(username:password)>`
- Roles: `system_admin` (full access including user management) and `admin` (standard management)
- Account lockout for LOCKOUT_DURATION after 5 failed login attempts
- Bootstrap: creates `admin` user from ADMIN_PASSWORD on first startup

**Ground Control API Routes** (spec/groundcontrol/openapi.yaml, security wrapping in internal/groundcontrol/server/routes.go):
- Public: `GET /ping`, `GET /health`, `POST /login` (rate limited), `POST /satellites/ztr` (rate limited), `GET /satellites/spiffe-ztr` (SPIFFE required, rate limited)
- Satellite-authenticated: `POST /satellites/sync` (SPIFFE SVID or robot Basic auth, rate limited); `POST /satellites/refresh` sits behind the same chain and answers 501 until D2 lands
- Protected (under `/api`): groups, configs, satellites, users, logout
- System admin only: user create/delete and admin password reset, group delete, `/api/spire/*`, `POST /api/satellites/register` (SPIFFE registration), `/api/ops/*` (`GET /api/ops/outbox` answers 501 until plan item 2.3)
- Background cleanup job: every 24h under a PostgreSQL advisory lock, deletes status records older than 7 days and orphaned artifacts

**SPIFFE/SPIRE Authentication** (satellite to Ground Control):
- mTLS using X.509 SVIDs from the SPIRE Workload API
- Attestation methods accepted by `POST /api/satellites/register`: join_token, x509pop, sshpop
- Embedded SPIRE (EMBEDDED_SPIRE_ENABLED) execs a `spire-server` binary with only the join_token node attestor and an in-memory key manager

### Build Tags

- Default build: full functionality including SPIFFE, AES config encryption and Linux device fingerprinting
- `nospiffe`: compiles the `*_stub.go` files (internal/shared/spiffe, internal/groundcontrol/spiffe, internal/groundcontrol/server/server_spiffe_stub.go). It also swaps crypto for a pass-through `NoOpProvider` (internal/shared/crypto/provider_stub.go) and device identity for `NoOpIdentity`, so `encrypt_config` stores plaintext. Use `go build -tags nospiffe ./...`
- Device identity is real only for `linux && !nospiffe`; all other platforms get the stub
- Release artifacts and images are built without tags. The Dockerfile's `GO_TAGS` comment still mentions `parsec`; that code was removed in #526

## Go Style Rules

- Go 1.26.5 (go.mod, CI, Dockerfile)
- Use `any` instead of `interface{}`.
- Use `t.TempDir()` in tests instead of manual temp paths with `os.TempDir()`.
- Use `cm.With()` modifiers for all ConfigManager mutations (never mutate via `cm.GetConfig()` directly).
- Satellite code uses the context-carried logger (`logger.FromContext(ctx)`). Ground Control handlers mostly use the stdlib `log` package.
- Periodic satellite tasks implement `process.Process` and run under `scheduler.Scheduler`.
- SPIFFE-dependent code: `feature.go` with `//go:build !nospiffe` and `feature_stub.go` with `//go:build nospiffe`.

## Important Development Notes

### State Management

The satellite keeps credentials and app config in `config.json` and the replicated group state in `state.json` (atomic write via temp file + rename). State is re-fetched from Harbor on every state replication tick; on shutdown the in-memory state is persisted.

`ConfigManager.GetRemoteRegistryCredentials()` returns the BYO `local_registry` credentials despite its name.

### Satellite CLI Flags

Flags default to their env var equivalents (see internal/shared/env/harbor-satellite.go):
- `--token` / `TOKEN`: Registration token (required unless SPIFFE is enabled)
- `--ground-control-url` / `GROUND_CONTROL_URL`: Always required
- `--harbor-registry-url` / `HARBOR_REGISTRY_URL`: Always required (except `--fallback-only`); overrides the Harbor host in credentials and state URLs
- `--json-logging`: JSON logging (default true, no env var)
- `--use-unsecure` / `USE_UNSECURE`: HTTP and skip TLS verification
- `--mirrors`: CRI mirror config (`CRI:registry1,registry2`), no env var
- `--spiffe-enabled`, `--spiffe-endpoint-socket`, `--spiffe-expected-server-id` / `SPIFFE_*`
- `--byo-registry`, `--registry-url`, `--registry-username`, `--registry-password` / `BYO_REGISTRY`, `REGISTRY_*`
- `--config-dir` / `CONFIG_DIR`, `--registry-data-dir` / `REGISTRY_DATA_DIR`
- `--shutdown-timeout` / `SHUTDOWN_TIMEOUT` (30s)
- `--no-registry-fallback` / `NO_REGISTRY_FALLBACK`
- `--fallback-only`: Apply CRI fallback configs and exit, no env var; rejected together with an explicit `--proxy-mode` / `PROXY_MODE`
- `--proxy-mode` / `PROXY_MODE` (proxy|replica), `--proxy-port` / `PROXY_PORT` (8585)
- `--direct-delivery` / `DIRECT_DELIVERY`, `--image-dir` / `IMAGE_DIR`: experimental k3s/RKE2 tarball delivery

### Container Runtime Integration

CRI configs make the runtime pull through the satellite with the upstream as fallback (containerd: `hosts.toml` with the upstream as `server` and the endpoint as a `pull`/`resolve` host). In a normal run the endpoint is the OCI proxy at `127.0.0.1:<proxy-port>`. With `--fallback-only` no proxy runs, so the endpoint is the BYO registry URL; without `--byo-registry` it prints a warning and skips. Priority: GC-delivered `registry_fallback` > `--mirrors` > `--no-registry-fallback`.
- Format: `--mirrors=<CRI>:<registry1>,<registry2>`
- Example: `--mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io`
- Docker only supports mirroring docker.io, use `--mirrors=docker:true`
- Requires write access to CRI config files (usually root); Docker needs a daemon restart

### Database Migrations

goose migrations live in `internal/groundcontrol/sql/schema/` (001 to 022, no 014) and run automatically on GC startup. The Dockerfile copies them to `/migrations`.

### Hot Reload

The satellite watches `config.json`. Log level, state replication interval and audit settings apply without restart; other changes need a restart.

## Testing

- Unit tests colocated with source files (*_test.go)
- E2E tests are Taskfile-driven shell flows (taskfiles/e2e.yml), not Go tests
- E2E compose stacks: `test/e2e/docker/docker-compose.yml`, `test/e2e/docker/docker-compose.spiffe.yml`
- Test configs in test/e2e/testconfig/, fixtures in test/e2e/testdata/
- GC handler tests use go-sqlmock. Integration tests in the same packages run against real Postgres when `TEST_DATABASE_URL` is set and skip otherwise; `task test:integration` starts `test/integration/docker-compose.yml` (postgres:17 with pg_stat_statements, host port `TEST_PG_PORT`, default 55434) and sets it
- `internal/groundcontrol/dbtest`: a fresh database per test with all goose migrations, plus pg_stat_statements counts per test database. `internal/groundcontrol/harbortest`: in-process Harbor API and registry stub that records every call and can fail, go down or hold requests
- `internal/groundcontrol/server`: SQL statement and Harbor call budgets (`budget_test.go`), golden responses of every endpoint in `testdata/golden` (refresh with `go test ./internal/groundcontrol/server -run TestGoldenAPI -update`). `test/contract`: the paths the satellite calls must be served by the GC router
- Tests for Harbor 3 plan items that have not landed (defects D1..D9, planned features) start with `pending.Plan` (`internal/shared/testutil/pending`) and skip unless `HARBOR3_PENDING=1`; the implementing change removes the marker

## CI/CD

GitHub Actions workflows:
- .github/workflows/test.yaml (PRs): unit tests + Codecov (ubuntu-latest); Ground Control integration tests via `task test:integration` (ubuntu-latest); govulncheck, GoReleaser snapshot, satellite and GC builds (self-hosted `container-registry`); E2E main, BYO, SPIFFE (ubuntu-latest). Crash-recovery E2E is not run in CI
- .github/workflows/lint.yaml (PRs): golangci-lint v2.12.2 binary on `container-registry` (no Docker daemon there, so it does not use `task lint-report`); job `arch` runs `task lint:arch`
- .github/workflows/release.yaml (push to main and `v*.*.*` tags, ubuntu-latest): per-arch builds (linux amd64/arm64/ppc64le/s390x/riscv64) of the `satellite` and `ground-control` images to `${REGISTRY_ADDRESS}/${PROJECT_NAME}` from repository variables (README: registry.goharbor.io/harbor-satellite), multi-arch manifest, Cosign keyless signing; tags additionally run `task release` (GoReleaser)
- .github/workflows/labeler.yaml (`container-registry`): auto-labels PRs (documentation for *.md, golang for *.go)

Key tasks: `build`, `build-all`, `lint`, `lint:arch`, `lint-report`, `vuln`, `vuln-report`, `test:integration`, `e2e*`, `generate:groundcontrol`, `byo-up`, `byo-down`, `publish`, `publish-and-sign`, `release`, `snapshot`, `clean`.

## Architecture Decisions

ADRs in docs/decisions/:
- ADR-0001: Skopeo vs Crane (accepted, chose Crane; ADR-0009 supersedes the target state)
- ADR-0002: Zot vs Docker Registry (superseded, Zot replaced by ORAS in #648)
- ADR-0003: Remote config injection (accepted, implemented with a different API than its examples)
- ADR-0004: Ground Control authentication (accepted; session-based auth with bearer tokens; partly superseded by ADR-0016 and ADR-0018)
- PDR-0005: SPIFFE identity and security (accepted)
- ADR-0006: Satellite lifecycle states (proposed; not implemented; ADR-0016 designs its implementation)
- ADR-0007, ADR-0008: PARSEC hardware-backed identity and bootstrapping flow (deprecated; PARSEC code was removed in #526). Related proposal in docs/proposals/
- ADR-0009: ORAS OCI storage and a policy-enforcing transparent proxy (proposed; ORAS store #648, proxy package #649, wired with proxy/replica modes in #654; CEL policy and cache mode not implemented). Its Implementation Status table predates #654
- ADR-0010: Peer-to-peer distribution between satellites (proposed; config block #674 and peer roster #676 only)
- ADR-0013: Edge store concurrency and pull-through streaming (proposed; skeleton only, see Harbor 3 skeleton below)
- ADR-0014: Ground Control async plane, transactional outbox and level-triggered publisher (proposed; skeleton only)
- ADR-0015: Edge journal and upload protocol (proposed; skeleton only; relation to open PR #597's ADR-0011 satellite eventing)
- ADR-0016: Satellite sessions and the fleet hot path (proposed; skeleton only; implements ADR-0006, supersedes the satellite part of ADR-0004)
- ADR-0017: Policy engine, Casbin for Ground Control and edge admission (proposed; skeleton only; supersedes ADR-0009's CEL section)
- ADR-0018: Operator identity through OIDC and satellite credential rotation (proposed; skeleton only)
- ADR-0019: Ground Control hosting and extension points (proposed; skeleton only; webhooks, event catalog, edge middleware seam)
- groundcontrol-internal-package-migration.md: moving GC into the root module (done)

## Common Workflows

### Adding a new API endpoint to Ground Control

1. Add the path and schemas to spec/groundcontrol/openapi.yaml
2. Run `task generate:groundcontrol` (server and client)
3. Implement the generated interface method in internal/groundcontrol/server/*_handlers.go
4. If the route needs non-default auth (public, satellite, system admin), update routeSecurityMiddleware / requiresSystemAdmin in routes.go
5. Add SQL in internal/groundcontrol/sql/queries (and a migration in sql/schema if needed), then `sqlc generate`
6. Add a CLI command under internal/groundcontrol/cli/root if operators need it

### Adding a new satellite feature

1. Implement in the appropriate internal/satellite/ package
2. Update pkg/config/ if configuration changes are needed, with a modifier in modifiers.go
3. Add validation and defaults in pkg/config/validate.go
4. Update cmd/satellite/main.go and internal/shared/env/harbor-satellite.go for new flags and env vars
5. Update examples/config.example.json and .env.example

### Modifying state replication logic

State replication in internal/satellite/state/ and internal/satellite/store/:
- fetcher.go: Fetching state artifacts from Harbor
- state_process.go: Orchestrating fetch, diff, delete/replicate, config reconcile, direct delivery
- store/store.go: Backend-neutral `Store` contract shared with the OCI proxy
- store/oci.go: Replicating OCI graphs into the local image layout (ORAS)
- store/registry.go: Remote registry store (ORAS) for Harbor and BYO registries
- proxy/process/image/pull.go: Proxy pull path; proxy-mode misses replicate through the same `Store.Replicate`
- direct_delivery.go: k3s/RKE2 tarball delivery
- registration_process.go, spiffe_registration.go: Token and SPIFFE ZTR
- reporting_process.go, report.go, catalog.go: Heartbeat, metrics, cached image list (BYO only)
- state_persistence.go: Atomic persistence to state.json
- helpers.go: URL validation, state fetcher creation

This is core functionality; changes require careful review.

### Docker Compose Files

- `docker-compose.yml`: dev stack (PostgreSQL + Ground Control, `satellite` behind the `satellite` profile), built from source. SKIP_HARBOR_HEALTH_CHECK defaults to true in the compose file; a root `.env` copied from `.env.example` sets it to false. The satellite's proxy binds the container's loopback, so it is not reachable from the host
- `docker-compose.byo.yml`: satellite + `registry:2` sidecar (`task byo-up` / `byo-down`)
- `test/e2e/docker/`: E2E stacks (standard and SPIFFE)
- `examples/deploy/`: token (no-spiffe) quickstart, SPIFFE join-token/sshpop/x509pop compose setups (GC on https://localhost:${GC_HOST_PORT:-9080}), GC Helm chart in `examples/deploy/helm/groundcontrol`

### Adding SPIFFE support to a new component

1. Create the main implementation file (e.g. `feature.go`) with `//go:build !nospiffe`
2. Create a stub (`feature_stub.go`) with `//go:build nospiffe` that returns an error or no-ops
3. Test both builds: `go build ./...` and `go build -tags nospiffe ./...`

## Harbor 3 skeleton (not wired)

Scaffolding for the Harbor 3 plan. Stubs return an error wrapping `notimpl.ErrNotImplemented` and no production path calls them yet; their behaviour is specified by `pending.Plan` tests.

- internal/shared/notimpl/: `ErrNotImplemented` and `Err(item)`, returned by every skeleton stub
- internal/satellite/gcclient/: D1, the single satellite→GC client over pkg/groundcontrol (token ZTR, SPIFFE ZTR, sync) with its own result types (no pkg/config import) and `Credentials` (`RobotBasic`, `NoCredentials`); the state processes still join URLs themselves
- D2: `POST /satellites/refresh` is routed behind satellite auth and answers 501 (`server/satellite_refresh.go`, `server/notimplemented.go`); `scheduler.NewScheduler` (no interval, used for the `refresh_credentials` event) still runs its process on Start; the fix makes it run only on Trigger
- D3: `server.satelliteAuthChain` is the seam for the satellite auth order; it keeps today's order (SPIFFE middleware inner), the fix swaps it
- internal/shared/logger/logtest/: D4 test helper, `Capture(t)` routes the context-carried zerolog logger into a buffer at every level; the D4 acceptance tests in `state` and `events` use it
- test/arch/: repository-wide tests; `TestNoSecretsInLogArguments` (D4) parses every non-test file in cmd, internal and pkg for log arguments named like a password, secret or token
- D5: `server.syncRobotPermissions` (for a set of satellites, one query and one Harbor update per robot with the union of the satellite's groups) and the query `GetProjectsOfSatellites`; D10 (empty satellite state artifact) has tests only
- D6: `server.verifyUserCredentials(ctx, clientIP, username, password)` (one credential check for Login and Basic auth that owns the lockout, the attempt records and the IP failure limiter) and `middleware.FailureLimiter` (per client IP, never blocks yet), built by `NewServer` into `Server.FailureLimiter` from AUTH_FAILURE_LIMIT (20) and AUTH_FAILURE_WINDOW (1m)
- D7: migration 016 adds `satellite_token.consumed_at`; queries `ClaimToken`, `UnclaimToken`, `ConsumeToken` and the stubs `server.claimZTRToken` / `unclaimZTRToken` (a consumed token is `errTokenInvalid` wrapping `errTokenConsumed`, answered 400 like an unknown one); Ztr still reads and deletes the token without a claim
- D8: query `TryAdvisoryXactLock`; `runCleanupWithLock` still takes a session advisory lock through the pool
- D9 and D12: `store.Store.Delete` documents the digest guard (an artifact's tag goes only while it still resolves to the artifact's digest, then unreferenced content is collected; `RegistryStore` deletes the artifact's own digest), which neither store implements yet; `state.applyGroupChanges` (replicate, then delete) and `state.withoutWanted` (removals checked against every group's wanted entities, D12) are stubs; `state_process.go` still deletes before it replicates, per group
- `.go-arch-lint.yml` (0.1, enforced): import boundaries checked by `task lint:arch` (go-arch-lint, pinned by `GO_ARCH_LINT_VERSION`, run with `go run`). `task lint` runs it before golangci-lint and the lint workflow runs it as job `arch`. Every package must belong to a component and a component may import its own packages only when it lists itself, so a new package needs an entry; test helper packages named `*test` fall into `testsupport` by themselves, and `internal/satellite/gcclient` is its own component that may import only `pkg/groundcontrol` and the shared packages. The two `EXCEPTION 0.1` entries go once `proxy.Mode` leaves `internal/satellite` and `pkg/config` gets an injected encryptor (`test/arch` pending test). `govet` is enabled with `shadow` off
- `internal/satellite/telemetry` (0.4): metric names, `Recorder` and `Nop`, and a stub `/healthz` + `/metrics` server. `TELEMETRY_ADDR` is parsed; nothing starts the server
- `internal/groundcontrol/dbpool` (3.2): one pool per plane (API, fleet, async); fleet and async use the API pool unless `DB_URL_FLEET`/`DB_URL_ASYNC` is set. `env.Database` parses `DB_URL_API|FLEET|ASYNC`, `DB_{API,FLEET,ASYNC}_*` pool settings and `DB_SSLMODE` (default `disable`; 3.3 makes it `require`)/`DB_SSLROOTCERT`, but GC still opens a single pool from `URL()` with `sslmode=disable`. lib/pq stays: it cancels server queries on context end (`TestLibPQCancelsQueryOnContextCancel`)
- `internal/groundcontrol/sql/pending` (3.4): constraints DDL and `UpdateConfigIfVersion`, read by neither goose nor sqlc (README there); `updateConfig` already documents its 409. `DeleteStaleLoginAttempts` (3.5) is generated, the cleanup does not call it yet. Reverting migrations below 015 fails (015's Down recreates `satellite_cached_images`); `TestMigrations_UpDownUp` is pending
- Domain and ports (1): `internal/groundcontrol/fleet` holds the domain types (typed IDs), sentinel errors, the ports `Registry`, `WorkloadIdentity`, `Repository`/`Tx` (`Autocommit()` for single statements; its event sink refuses appends) and `EventSink` (`NopEventSink` until plan item 2), the event payloads (frozen JSON, every payload names its aggregate), `Principal` in the context (`ContextWithPrincipal`/`PrincipalFrom`) with `Deps.Policy` (default `AllowAll`), `Settings` with the state refs, `RenderSatelliteState`/`RenderGroupState`, the robot rule `RobotProjects` over the batch `SatelliteRepo.RobotGrants`, and the services `Satellites`, `Groups`, `Configs`, `Registration` behind `NewServices`. `fleet/fleettest` has recording fakes (one shared `Recorder`, `FailOn`, seeded lookups; writes change nothing). Adapters: `adapters/harbor2` (builds its own client from `Config`, reads no env), `adapters/postgres`, `adapters/spire`; `internal/groundcontrol/app.NewFleet` composes them. `server.NewServer` takes options (`WithFleet`); `server.StatusForError` and `AppErrorFor` map the fleet errors, keeping today's token error bodies. Handlers still call sqlc and the global `harbor.GetClient`, and `serve` builds no services. `fleet.Artifact` encodes like `server.GroupArtifact` (`TestFleetArtifactEncodesLikeGroupArtifact`, `TestGroupStateGoldenDigest`; satellite states with and without groups in `TestSatelliteStateGoldenDigests`); D7's token errors are `fleet.ErrTokenInvalid`/`ErrTokenExpired`
- Async plane (2, ADR-0014 proposed): migration 017 adds `event_outbox`, `group_artifacts` (the source of a group's artifacts) and the derived `desired_state` (rendered, published and published-artifact digests per aggregate), with SQL-contract-tested queries (claim, release, rearm, log-only append, one-statement group fan-out, aggregate locks). `internal/groundcontrol/async` declares the publisher (claims under a per-aggregate session lock, releases on registry outages and lock contention, pauses while the probe is stale), the variadic router, the satellite/group/config reconcilers, the tombstone handler, the worker, `RegistryProbe` and `LimitRegistry` (stubs); `adapters/postgres` has `OutboxStore`, `OutboxSink`, `DesiredStateStore` and `AggregateLocker` (stubs). `app.NewFleet` returns `Fleet{Services, Probe}` with `ServerOptions`, `app.NewWorker` composes the worker, both on `dbpool.Pools` with `app.Settings` (stubs). The fleet service tests describe the 2.5 target (one transaction plus events, no registry call except ZTR's rotation). `serve --worker=off|embedded` (default `off`; `embedded` and the hidden `groundcontrol worker` answer not implemented). The spec has `GET /api/ops/outbox` (501), ZTR 503 with `Retry-After` and `/health` `degraded` with `components`; the server takes `HarborContact` and `OutboxReader` implementations as options and never imports the async plane. The server harness's `registerSatellite` calls `drainOutbox` (a no-op until 2.5 moves robot creation to the worker). Every advisory lock key (the cleanup's ID, the aggregate namespaces) is in `internal/groundcontrol/advisorylock`. Handlers still call Harbor in the request, `serve` still checks Harbor at startup and ZTR still returns the placeholder secret under `SKIP_HARBOR_HEALTH_CHECK`
- Edge pull path (5, ADR-0013 proposed): `store.OpenOCIStore` (atomic `index.json`, torn-index recovery), `CollectGarbage` with `FillLeases`/`RefCounter`, the optional `Filler` capability, `state.ResetReplicatedEntities`, `internal/satellite/lane`, `image.NewPullWithOptions`/`NewLanePrefetcher` (zero options are `NewPull`) and `proxy.WithWriteDeadline`. `cmd/satellite` still calls `NewOCIStore` and `NewPull`; `PROXY_MAX_CONNECTIONS`, `PROXY_WRITE_TIMEOUT`, `PROXY_STREAMING` and `PROXY_PREFETCH_CONCURRENCY` are parsed only
- Edge cold path and journal (6, ADR-0015 proposed): `internal/shared/edgejournal` is the shared vocabulary (record types `replication`/`cri`/`pull_through`/`dropped`, versioned payloads with ADR-0011 field names, golden JSON tests). `internal/satellite/journal` (segments under `PathConfig.JournalDir`, group commit, non-blocking `TryAppend` whose drops become one counted `dropped` record, a time-ordered epoch, ack and a floor that never decreases), `state.StateArtifactChanged` (6.1), `store.RegistryOptions.BandwidthBytesPerSec`, `SetReplicationSource` and `lane.ThrottledTransport` (6.2), `state.RunSummary` with `Applied` and `LastRun` (6.6), `scheduler.TryTrigger` and `events.EventScheduler.Stop` (6.7); `SetJournal`, `SetReplicationSource` and `SetRunSummarySource` are no-ops. The app_config keys `replication_concurrency`, `replication_bandwidth_bytes_per_sec` and `journal` (`max_segment_bytes`, `max_total_bytes`, `upload_batch`) are accepted but not read. GC: migration 018 (`satellite_journal_cursor` only) with `OpenJournalCursor` (GREATEST upsert that reports a fresh cursor) and `AdvanceJournalCursor` (moves the cursor and appends the edge events, log-only, in one statement); the sync request fields `journal {epoch, after_seq, records}`, `applied_states` and `last_replication_error`, the response's `journal {epoch, acked_seq, error}` and a 413 for bodies over `SYNC_MAX_BODY_BYTES`, none of which the handler reads, sets or enforces yet; `fleet.JournalService` (`NewJournalService`, `Ingest`, a dependency of plan item 4's heartbeat service, not in `Services`), `fleet.JournalRepo` (`Tx.Journal`, `Advance` takes edge events only) with a postgres stub, `fleet.NewEdgeEvent` and the log-only edge kinds (`EdgeReplicated`, `EdgeDeleted`, `EdgeArtifactFailed`, ...), whose emission must not ship before 20's outbox retention. `cmd/satellite` opens no journal, every group still replicates in its own goroutine through the shared Harbor store and the heartbeat still calls the blocking `Trigger`
- Fleet hot path (4, ADR-0016 proposed): migration 019 adds `satellite_sessions` (SHA-256 of `hss_` tokens, one successor per session), `satellite_identities` (SPIFFE IDs) and the read model `satellite_view` (lifecycle state, applied digests and states, inventory digest and artifact IDs), with SQL-contract-tested queries; `sql/pending/satellite_status_partitions.sql` (4.5). `internal/groundcontrol/authn` (`NewSessionToken`, `HashSessionToken`, `Sessions` with a positive cache bounded by `MaxSessionCacheTTL`), `internal/groundcontrol/heartbeat` (`Buffer`, `Flusher`), `internal/shared/inventory` (`Digest`), `fleet.HeartbeatService` (`Services.Heartbeats`, owns the `JournalService`), lifecycle types and `ViewRepo` (`Tx.Views`), `SatelliteService.Revoke`/`Unrevoke`/`View`/`ListByState`/`Inventory`, `SatelliteRepo.SetIdentity`, `Registry.SetRobotAccess` (projects and disabled flag in one PUT), `async.StaleDetector` and `SessionExpirer`, postgres `SessionStore`, `HeartbeatStore`, `PartitionMaintainer` and view repository, `middleware.Limiter`/`LimitBy`, all stubs; `middleware.ParseRate` is written. Server: `WithSatelliteAuthenticator` switches `satelliteAuthChain` to SPIFFE, `authenticateSatellite` and the fleet limiter, and sync to `syncWithAuth` (both 501); nothing passes it, so the chain is unchanged. `RATE_LIMIT_SPLIT=true` builds `AnonymousLimiter` and `FleetLimiter`, whose `LimitBy` answers 501, so the flag stays off until 4.6. The spec has `session` in ZTR and sync responses, `inventory_requested`, `inventory_digest` and `POST /api/satellites/{satellite}/revoke`, `/unrevoke` and `GET .../lifecycle` (501). Satellite: `config.GCSession` (`state_config.gc_session`), `gcclient.Session` and `SessionBearer`, `state.InventoryDigest` (stub). The heartbeat still verifies the robot in 8 statements; `test/load` (`task test:load`) is the 5000-satellite load test
- Policy (7, ADR-0017 proposed): migration 020 adds `policy_rules`, `policy_role_bindings` and `policy_version`, with SQL-contract-tested queries and `ListGroupNamesOfSatellites`. `fleet.Policy` gains `AuthorizeType`; resource types, actions and `ScopedTypes()`; `PolicyService` (`Services.Policies`), `PolicyRepo` (`Tx.Policies`), `GroupRepo.GroupsOfSatellites`, log-only `PolicyChanged`; `ConfigValidator` (`Deps.ConfigValidator`) with `ConfigService.Create`/`Update` returning `ConfigWriteResult`. `adapters/casbin` (embedded model and baseline, `RouteMatcher`, poller), postgres policy repository, `internal/groundcontrol/satconfig` validator, all stubs. Server: `PolicyRoutes` maps every `/api` route to a resource type and action (`TestPolicyRoutes_CoverGeneratedRouter`); `PolicyMiddleware` replaces the system_admin check only with `WithPolicy` and `POLICY_ENGINE=casbin`; `requiresSystemAdmin` covers `/api/policies` and `/api/role-bindings`, whose handlers answer 501; config writes document 422. Edge: `internal/shared/policybundle`, `internal/satellite/admission` (evaluator, middleware, config source) and `app_config.admission`, not used by `cmd/satellite`
- Identity (8, ADR-0018 proposed): migration 021 adds `user_identities`, `oidc_login_states` and `robot_rotations`, with SQL-contract-tested queries (`UpsertOIDCUser` never adopts a local account, single-use `TakeOIDCLoginState` bound to the browser, `GetSessionPrincipalByToken` with OIDC groups, rotation stage/ack/promote, `ListRobotHarborIDsOfSatellite`). `authn` OIDC (`OIDCConfig`, `OIDCProvider`, `IdentityStore`, `MapPrincipal`, `OIDCLogin`), `adapters/oidc` (no go-oidc yet), postgres `IdentityStore` and `RotationStore`, `internal/groundcontrol/secrets` (AES-GCM sealer), `async.RobotRotator`, `RotationCache` and `LoginStateExpirer`, `fleet` rotation types (`RotationSource`, `Sealer`, `SuccessorRobotName`, `RobotRotated`, `RobotRepo.HarborIDs`, heartbeat `CredentialsGeneration`/`Credentials`), all stubs. Server: `/oidc/login`, `/oidc/callback`, `/oidc/device`, `/oidc/device/token` answer 501 behind the anonymous limiter, `WithOperatorLogin`, `errPasswordLoginDisabled`. CLI `auth login --oidc` answers not implemented. Satellite: `internal/satellite/registryauth` (`StaticProvider` written, `WorkloadIdentityProvider` stub), `gcclient.RotatedCredentials`, `state.ApplyRotatedCredentials` (stub), `app_config.registry_auth`, `state_config.credentials_generation`. D2 still rotates in the request
- Extension points (9, ADR-0019 proposed): migration 022 adds the `(xid, id)` index on `event_outbox`, `webhook_subscriptions` and `webhook_checkpoints`, with SQL-contract-tested queries (`ReadOutboxAfter` with the xmin guard, `CreateWebhookSubscription` starting at the current xmin, never-backwards `AdvanceWebhookCheckpoint`, `FailWebhookDelivery` that disables after `WEBHOOK_DISABLE_AFTER`, `PruneDoneOutboxEvents` that keeps rows above enabled checkpoints). `internal/groundcontrol/webhook` (ports, `CloudEvent`, `EventType` written, `Sign`, `HTTPSender`, `Dispatcher`), `fleet.WebhookService` (`Services.Webhooks`, `Settings.WebhooksEnabled`, `ResourceWebhook`), `fleet.RegistryEventSource` (port and fake, 9.5), `async.OutboxStore.Prune` and `OutboxPruner`, postgres `WebhookRepo`, `WebhookStore`, `OutboxLog`, `SubscriptionLocker`, all stubs; advisory lock `OutboxPruner` and namespace `WebhookSubscriptions`. `/api/webhooks` routes are system admin only and answer 501. `fleet.EventPayloads` and `spec/events/v1` (one JSON Schema per kind, checked by `test/contract`) are the event catalog. `internal/satellite/proxy/middleware` (`Stack` written, `Authenticate` and `Metrics` stubs)
