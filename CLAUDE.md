# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Harbor Satellite is a registry fleet management and artifact distribution solution that extends Harbor container registry to edge computing environments. Two main components:

1. Satellite: Runs at edge locations and replicates OCI content from Harbor into a local ORAS OCI image layout (default) or an external BYO registry
2. Ground Control (GC): Cloud-side management service for device management, onboarding, state management, and artifact orchestration. Ships with a companion CLI (`groundcontrol`)

## Build and Development Commands

### Building

```bash
# Build satellite, Ground Control server, and Ground Control CLI into bin/
task build

# Build individual components
task _build:satellite            # bin/satellite
task _build:ground-control       # bin/ground-control
task _build:groundcontrol-cli    # bin/groundcontrol

# Cross-platform builds (linux amd64/arm64/ppc64le/s390x/riscv64, darwin amd64/arm64)
task build-all

# Run satellite directly
go run ./cmd/satellite --token "<token>" --ground-control-url "<url>" --harbor-registry-url "<harbor-url>"

# Run Ground Control directly (reads .env from the working directory)
go run ./cmd/groundcontrol/server

# Run the Ground Control CLI
go run ./cmd/groundcontrol/cli --help
```

### Code Generation

```bash
# OpenAPI server (internal/groundcontrol/server/server.gen.go) and client (pkg/groundcontrol/client.gen.go)
task generate:ground-control     # alias gen:gc; also gen:gc-server, gen:gc-client

# sqlc: queries in internal/groundcontrol/sql/queries, schema in internal/groundcontrol/sql/schema
sqlc generate                    # config in sqlc.yaml, output in internal/groundcontrol/database/
```

The spec is `spec/ground-control/openapi.yaml` (oapi-codegen configs in `spec/ground-control/oapi-codegen/`). Route matching, parameter binding and request/response types come from the generated server; do not hand-edit `*.gen.go` or `internal/groundcontrol/database/*.sql.go`.

### Testing

```bash
# Run all tests
go test ./... -v -count=1

# Run a single test
go test -v -run TestFunctionName ./path/to/package

# Run E2E tests (main, BYO, SPIFFE; crash-recovery variants run separately)
task e2e

# Run individual E2E variants
task e2e-test               # Standard E2E (local OCI layout)
task e2e-byo                # BYO registry E2E
task e2e-spiffe             # SPIFFE join-token E2E
task e2e-crash-recovery     # Crash recovery with the local OCI store
task e2e-crash-recovery-byo # Crash recovery with BYO registry
```

### Linting

```bash
task lint          # golangci-lint in a pinned Docker image
task lint-fix
task lint-report   # writes golangci-lint.report
task vuln          # govulncheck in a pinned Docker image
```

Single config `.golangci.yaml` (golangci-lint v2, CI pins v2.12.2) with 50 enabled linters plus gofmt, gofumpt and goimports formatters. Notable enabled linters: gosec, errcheck (check-blank, type assertions), errorlint, depguard (bans io/ioutil), interfacebloat (max 10 methods), nakedret (no naked returns at all), tagalign, nolintlint (requires explanation). `cyclop`, `funlen`, `gochecknoglobals` and `gochecknoinits` have settings in the file but are not enabled.

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

# Satellite with CRI mirror config (requires --byo-registry, see Container Runtime Integration)
go run ./cmd/satellite --token "<token>" --ground-control-url "<url>" --harbor-registry-url "<harbor-url>" \
  --byo-registry --registry-url "<registry>" --mirrors=containerd:docker.io,quay.io

# Ground Control with Go (requires .env, see .env.example)
go run ./cmd/groundcontrol/server
```

## Architecture

### Module Structure

One Go module at the repository root (`github.com/container-registry/harbor-satellite`, `go 1.26.5`). Satellite, Ground Control server and Ground Control CLI are separate binaries built from it. Keep entrypoints in `cmd/` and implementation under `internal/`; `pkg/` holds the satellite config package and the generated GC client.

- cmd/satellite/: satellite entrypoint
- cmd/groundcontrol/server/: Ground Control server entrypoint
- cmd/groundcontrol/cli/: `groundcontrol` CLI entrypoint (cobra)

### Satellite Component Structure

- cmd/satellite/main.go: CLI flags and env vars, CRI config application, direct-delivery setup, audit logger, config watcher, graceful shutdown
- pkg/config/: Configuration management (ConfigManager), validation and default enforcement, path resolution, optional encryption on write
- internal/satellite/satellite.go: Orchestration: picks token or SPIFFE ZTR, starts ZTR, state replication and status report schedulers, registers events
- internal/satellite/state/: State fetching, replication, token and SPIFFE registration, status reporting, state persistence, direct delivery
- internal/satellite/store/: `Store` interface; `OCIStore` (ORAS OCI image layout, default) and `RegistryStore` (go-containerregistry copy to a BYO registry)
- internal/satellite/events/: Event scheduler for GC-triggered one-shot jobs (currently only `refresh_credentials`)
- internal/satellite/scheduler/: Interval scheduler (`@every <duration>` parsed into a time.Ticker, runs once immediately on start)
- internal/satellite/process/: `Process` interface (Name, Execute, IsRunning, IsComplete)
- internal/satellite/container_runtime/: CRI config management (containerd, CRI-O, Docker, Podman) and k3s/RKE2 image dir detection
- internal/satellite/hotreload/, internal/satellite/watcher/: Config file watching and hot reload (log level, state replication interval; audit config is swapped in cmd/satellite)
- internal/satellite/identity/: Device fingerprint (machine-id, MAC, disk serial); real only on Linux default builds
- internal/satellite/secure/: Device-bound config encryption (AES-256-GCM, Argon2id key from fingerprint)
- internal/satellite/tls/: Client TLS config loading
- internal/satellite/proxy/: OCI Distribution request parser and handler chain for the ADR-0009 proxy. Not wired into cmd/satellite yet

### Shared Packages

- internal/shared/env/: All env var parsing for satellite and GC (caarlos0/env struct tags)
- internal/shared/logger/: zerolog logger (context-carried) and audit logger (syslog daemon/network/file, OTLP/HTTP)
- internal/shared/crypto/: AES-GCM provider, Argon2id helpers
- internal/shared/spiffe/: SPIFFE Workload API client (X.509 SVID, mTLS HTTP client)
- internal/shared/utils/: Misc utilities (signal context, URL formatting, file I/O)
- internal/version/: Version and GitCommit, set via ldflags

### Ground Control Component Structure

- cmd/groundcontrol/server/main.go: Loads env, checks Harbor health, runs migrations, starts HTTP/TLS/SPIFFE server and the cleanup job
- internal/groundcontrol/server/: Handlers (satellites, groups, configs, auth, users, SPIRE), generated router (`server.gen.go`), per-route security middleware (`routes.go`), cleanup job
- internal/groundcontrol/database/: sqlc-generated PostgreSQL access (do not edit)
- internal/groundcontrol/sql/: `schema/` (goose migrations, also the sqlc schema) and `queries/`
- internal/groundcontrol/migrator/: goose runner; reads `/migrations` in the container image, else `internal/groundcontrol/sql/schema`
- internal/groundcontrol/harbor/: Harbor v2 API client (projects, robots, replication)
- internal/groundcontrol/harborhealth/: Startup Harbor health check (skippable via SKIP_HARBOR_HEALTH_CHECK)
- internal/groundcontrol/auth/: Password policy validation
- internal/groundcontrol/middleware/: Rate limiting, TLS cert watching
- internal/groundcontrol/spiffe/: SPIFFE provider, middleware, authorizer, embedded SPIRE server, SPIRE server client
- internal/groundcontrol/cli/: `groundcontrol` CLI commands (auth, get, create, update, delete, register, add, remove, sync, ping, health)
- internal/groundcontrol/utils/: State/config artifact URL helpers, robot project updates
- internal/groundcontrol/logger/: Unused copy of internal/shared/logger audit code
- pkg/groundcontrol/: Generated Go client for the GC API

### Key Concepts

Groups: Collections of artifacts (repository, tag, digest, type) that satellites replicate. GC pushes each group's state as an OCI artifact into Harbor under `satellite/group-state/<group>/state`.

Configs: Named satellite app configs (intervals, metrics, BYO registry, registry fallback, audit, TLS, etc.) delivered through Harbor as config state artifacts. Updates use JSON Merge Patch.

State Replication: The satellite fetches its root state artifact (`satellite/satellite-state/<name>/state`) directly from Harbor with its robot credentials, then fetches each group state and replicates changes into the store. GC is not on the replication data path.

Registration (ZTR, Zero Touch Registration): Two mutually exclusive paths:
- Token ZTR: Satellite POSTs `{token}` to `/satellites/ztr` and receives Harbor robot credentials and its state URL. Tokens expire after 24h and are deleted after a successful ZTR. Managed in state/registration_process.go.
- SPIFFE ZTR: Satellite presents an X.509 SVID via mTLS to `GET /satellites/spiffe-ztr` and is auto-registered. Managed in state/spiffe_registration.go.

Heartbeat and Status Reporting: The satellite POSTs to `/satellites/sync` (SPIFFE identity or robot Basic auth) with CPU, memory, storage, image count, cached images, sync duration and CRI activity. The response can carry events (e.g. `refresh_credentials` when the robot expires within two heartbeat intervals). Managed in state/reporting_process.go and state/report.go.

Stale detection: A satellite is stale when `last_seen` is older than 3x its reported heartbeat interval (15 min if not `@every`). `STALE_THRESHOLD` is parsed but not used by any query.

BYO Registry: `--byo-registry --registry-url ...` replicates into an external registry instead of the local OCI layout. This is the only mode that exposes a pullable registry endpoint.

Direct Delivery (experimental): `--direct-delivery` writes docker-save tarballs pulled from Harbor into the k3s/RKE2 agent images directory.

### Configuration Files

Satellite config directory defaults to `os.UserConfigDir()/satellite` (`~/.config/satellite` on Linux, `~/Library/Application Support/satellite` on macOS), overridable via `--config-dir` / `CONFIG_DIR`. It contains `config.json`, `prev_config.json`, `state.json` and the OCI layout in `oci/` (override with `--registry-data-dir` / `REGISTRY_DATA_DIR`).

`config.json` has two sections:
- state_config: Harbor robot credentials (`auth`) and the satellite state URL (`state`)
- app_config: Ground Control URL, log level, intervals, metrics, BYO registry (`bring_own_registry`, `local_registry`), TLS, SPIFFE, `encrypt_config`, `registry_fallback`, `harbor_registry_url`, `direct_delivery`, `audit`

Example: `examples/config.example.json`. Fallback intervals when missing or invalid: state replication `@every 30s`, registration `@every 5s`, heartbeat `@every 30s` (pkg/config/constants.go).

Config encryption is opt-in via `app_config.encrypt_config` (default false) and is read once when the ConfigManager is created.

### Environment Variables

Both binaries load `.env` from the working directory if present. Template: `.env.example`. Source of truth: `internal/shared/env/`.

Satellite (each also has a CLI flag, see Satellite CLI Flags): TOKEN, GROUND_CONTROL_URL, HARBOR_REGISTRY_URL, USE_UNSECURE, SPIFFE_ENABLED, SPIFFE_ENDPOINT_SOCKET, SPIFFE_EXPECTED_SERVER_ID, BYO_REGISTRY, REGISTRY_URL, REGISTRY_USERNAME, REGISTRY_PASSWORD, CONFIG_DIR, REGISTRY_DATA_DIR, SHUTDOWN_TIMEOUT (30s), NO_REGISTRY_FALLBACK, DIRECT_DELIVERY, IMAGE_DIR.

Ground Control:
- Harbor: HARBOR_URL, HARBOR_USERNAME, HARBOR_PASSWORD, ROBOT_DURATION_DAYS (30), SKIP_HARBOR_HEALTH_CHECK (false, testing only)
- Database: DB_HOST, DB_PORT, DB_DATABASE, DB_USERNAME, DB_PASSWORD (sslmode=disable)
- Server and auth: PORT (8080), ADMIN_PASSWORD (bootstraps `admin`), SESSION_DURATION (24h), LOCKOUT_DURATION (5m), STALE_THRESHOLD (1h, currently unused)
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
- Account lockout after failed login attempts (LOCKOUT_DURATION)
- Bootstrap: creates `admin` user from ADMIN_PASSWORD on first startup

**Ground Control API Routes** (spec/ground-control/openapi.yaml, security wrapping in internal/groundcontrol/server/routes.go):
- Public: `GET /ping`, `GET /health`, `POST /login`, `POST /satellites/ztr` (rate limited), `GET /satellites/spiffe-ztr` (SPIFFE required, rate limited)
- Satellite-authenticated: `POST /satellites/sync` (SPIFFE SVID or robot Basic auth, rate limited)
- Protected (under `/api`): groups, configs, satellites, users, logout
- System admin only: user create/delete and admin password reset, group delete, `/api/spire/*`, `POST /api/satellites/register` (SPIFFE registration)
- Background cleanup job: every 24h, deletes status records older than 7 days and orphaned artifacts

**SPIFFE/SPIRE Authentication** (satellite to Ground Control):
- mTLS using X.509 SVIDs from the SPIRE Workload API
- Attestation methods accepted by `POST /api/satellites/register`: join_token, x509pop, sshpop
- Embedded SPIRE (EMBEDDED_SPIRE_ENABLED) execs a `spire-server` binary with only the join_token node attestor and an in-memory key manager

### Build Tags

- Default build: full functionality including SPIFFE, AES config encryption and Linux device fingerprinting
- `nospiffe`: compiles the `*_stub.go` files (internal/shared/spiffe, internal/groundcontrol/spiffe, internal/groundcontrol/server/server_spiffe_stub.go). It also swaps crypto for a pass-through `NoOpProvider` (internal/shared/crypto/provider_stub.go) and device identity for `NoOpIdentity`, so `encrypt_config` stores plaintext. Use `go build -tags nospiffe ./...`
- Device identity is real only for `linux && !nospiffe`; all other platforms get the stub
- Release artifacts and images are built without tags

## Go Style Rules

- Go 1.26.5 (go.mod, Taskfile GO_VERSION, CI, Dockerfile)
- Use `any` instead of `interface{}`.
- Use `t.TempDir()` in tests instead of manual temp paths with `os.TempDir()`.
- Use `cm.With()` modifiers for all ConfigManager mutations (never mutate via `cm.GetConfig()` directly).
- Satellite code uses the context-carried logger (`logger.FromContext(ctx)`). Ground Control handlers mostly use the stdlib `log` package.
- Periodic satellite tasks implement `process.Process` and run under `scheduler.Scheduler`.
- SPIFFE-dependent code: `feature.go` with `//go:build !nospiffe` and `feature_stub.go` with `//go:build nospiffe`.

## Important Development Notes

### State Management

The satellite keeps credentials and app config in `config.json` and the replicated group state in `state.json` (atomic write). State is re-fetched from Harbor on every state replication tick; on shutdown the in-memory state is persisted.

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
- `--fallback-only`: Apply CRI fallback configs and exit, no env var
- `--direct-delivery` / `DIRECT_DELIVERY`, `--image-dir` / `IMAGE_DIR`: experimental k3s/RKE2 tarball delivery

### Container Runtime Integration

CRI mirror config only applies in BYO registry mode: without `--byo-registry` cmd/satellite prints a warning and skips it, because the local OCI layout is not a registry endpoint. Priority: GC-delivered `registry_fallback` > `--mirrors` > `--no-registry-fallback`.
- Format: `--mirrors=<CRI>:<registry1>,<registry2>`
- Example: `--mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io`
- Docker only supports mirroring docker.io, use `--mirrors=docker:true`
- Requires write access to CRI config files (usually root); Docker needs a daemon restart

### Database Migrations

goose migrations live in `internal/groundcontrol/sql/schema/` (001 to 015, no 014) and run automatically on GC startup. The Dockerfile copies them to `/migrations`.

### Hot Reload

The satellite watches `config.json`. Log level, state replication interval and audit settings apply without restart; other changes need a restart.

## Testing

- Unit tests colocated with source files (*_test.go)
- E2E tests are Taskfile-driven shell flows (taskfiles/e2e.yml), not Go tests
- E2E compose stacks: `test/e2e/docker/docker-compose.yml`, `test/e2e/docker/docker-compose.spiffe.yml`
- Test configs in test/e2e/testconfig/, fixtures in test/e2e/testdata/
- GC handler tests use go-sqlmock

## CI/CD

GitHub Actions workflows:
- .github/workflows/test.yaml (PRs): unit tests + Codecov (ubuntu-latest); govulncheck, GoReleaser snapshot, satellite and GC builds (self-hosted `container-registry`); E2E main, BYO, SPIFFE (ubuntu-latest). Crash-recovery E2E is not run in CI
- .github/workflows/lint.yaml (PRs): golangci-lint v2.12.2 binary on `container-registry` (no Docker daemon there, so it does not use `task lint-report`)
- .github/workflows/release.yaml (push to main and `v*` tags, ubuntu-latest): per-arch image builds (linux amd64/arm64/ppc64le/s390x/riscv64) of `satellite` and `ground-control` to `${REGISTRY_ADDRESS}/${PROJECT_NAME}` (registry.goharbor.io/harbor-satellite), multi-arch manifest, Cosign keyless signing; tags additionally run `task release` (GoReleaser)
- .github/workflows/labeler.yaml: auto-labels PRs (documentation for *.md, golang for *.go)

Key tasks: `build`, `build-all`, `lint`, `lint-report`, `vuln`, `vuln-report`, `e2e*`, `generate:ground-control`, `publish`, `publish-and-sign`, `release`, `snapshot`, `clean`.

## Architecture Decisions

ADRs in docs/decisions/:
- ADR-0001: Skopeo vs Crane (chose Crane; historical, superseded by ADR-0009)
- ADR-0002: Zot vs Docker Registry (historical, superseded by ADR-0009)
- ADR-0003: Remote config injection (proposed; API-based config delivery with rollback)
- ADR-0004: Ground Control authentication (session-based auth with bearer tokens)
- PDR-0005: SPIFFE identity and security (accepted)
- ADR-0006: Satellite lifecycle states (proposed; not implemented)
- ADR-0007, ADR-0008: PARSEC hardware-backed identity and bootstrapping flow (proposed; PARSEC code was removed in #526). Related proposal in docs/proposals/
- ADR-0009: ORAS OCI storage and a policy-enforcing transparent proxy (proposed; ORAS store landed in #648, proxy package in #649 is not wired)
- ground-control-internal-package-migration.md: moving GC into the root module (done)

## Common Workflows

### Adding a new API endpoint to Ground Control

1. Add the path and schemas to spec/ground-control/openapi.yaml
2. Run `task generate:ground-control` (server and client)
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
- store/store.go: Backend-neutral `Store` contract
- store/oci.go: Replicating OCI graphs into the local image layout (ORAS)
- store/registry.go: Replicating images to an external BYO registry (go-containerregistry)
- direct_delivery.go: k3s/RKE2 tarball delivery
- registration_process.go, spiffe_registration.go: Token and SPIFFE ZTR
- reporting_process.go, report.go, catalog.go: Heartbeat, metrics, cached image list (BYO only)
- state_persistence.go: Atomic persistence to state.json
- helpers.go: URL validation, state fetcher creation

This is core functionality; changes require careful review.

### Docker Compose Files

- `docker-compose.yml`: dev stack (PostgreSQL + Ground Control, `satellite` behind the `satellite` profile), built from source. SKIP_HARBOR_HEALTH_CHECK defaults to true in the compose file; a root `.env` copied from `.env.example` sets it to false
- `docker-compose.byo.yml`: satellite + `registry:2` sidecar (`task byo-up` / `byo-down`)
- `test/e2e/docker/`: E2E stacks (standard and SPIFFE)
- `examples/deploy/`: token (no-spiffe) quickstart, SPIFFE join-token/sshpop/x509pop compose setups (GC on https://localhost:${GC_HOST_PORT:-9080}), GC Helm chart in `examples/deploy/helm/ground-control`

### Adding SPIFFE support to a new component

1. Create the main implementation file (e.g. `feature.go`) with `//go:build !nospiffe`
2. Create a stub (`feature_stub.go`) with `//go:build nospiffe` that returns an error or no-ops
3. Test both builds: `go build ./...` and `go build -tags nospiffe ./...`
