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

# Run E2E tests (main, BYO, SPIFFE, multi-group, Harbor outage, lifecycle; crash-recovery variants run separately)
task e2e

# Run individual E2E variants
task e2e-test               # Standard E2E (local OCI layout)
task e2e-byo                # BYO registry E2E
task e2e-spiffe             # SPIFFE join-token E2E
task e2e-crash-recovery     # Crash recovery with the local OCI store
task e2e-crash-recovery-byo # Crash recovery with BYO registry
task e2e-multi-group        # Satellite in two groups replicates from both projects
task e2e-harbor-outage      # GC starts and accepts writes with Harbor down, then converges
task e2e-lifecycle          # Revoked satellite gets 403 and recovers; webhooks once across a worker restart
# E2E_HARBOR_HOST_PORT (default 8080) moves the bundled Harbor's host port

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
- internal/satellite/peers/: `EligiblePeers` builds the peer allow-list from `peer_distribution` (merges static_peers and gc_peers, applies the group filter); `state.wrapWithPeerFill` turns it into the `store.PeerFillingStore` of a replication run
- internal/satellite/events/: Event scheduler for GC-triggered one-shot jobs (currently only `refresh_credentials`)
- internal/satellite/scheduler/: Interval scheduler (`@every <duration>` parsed into a time.Ticker, runs once immediately on start); `NewScheduler` without an interval runs its process only on `Trigger` (events such as `refresh_credentials`)
- internal/satellite/process/: `Process` interface (Name, Execute, IsRunning, IsComplete)
- internal/satellite/container_runtime/: CRI config management (containerd, CRI-O, Docker, Podman) and k3s/RKE2 image dir detection
- internal/satellite/hotreload/, internal/satellite/watcher/: Config file watching and hot reload (log level, state replication interval; audit config is swapped in cmd/satellite)
- internal/satellite/identity/: Device fingerprint (machine-id, MAC, disk serial); real only on Linux default builds
- internal/satellite/secure/: Device-bound config encryption (AES-256-GCM, Argon2id key from fingerprint)
- internal/satellite/tls/: Client TLS config loading
- internal/satellite/telemetry/: `/healthz` (checks `store`, `proxy`) and Prometheus `/metrics` on `TELEMETRY_ADDR`, private registry, `Recorder` passed to the OCI store and the pull handler
- internal/satellite/journal/: durable edge journal (segments in `<config dir>/journal`, group commit, non-blocking `TryAppend`, epochs, floor); `state` uploads it with the heartbeat (ADR-0015)
- internal/satellite/lane/: bounded background lane (prefetch of streaming pull-through, `ThrottledTransport` for the replication bandwidth cap)
- internal/satellite/admission/: edge admission evaluator and proxy middleware over `internal/shared/policybundle`, mode and bundle from `app_config.admission`
- internal/satellite/registryauth/: `Provider` port for Harbor credentials; `StaticProvider` (robot), `WorkloadIdentityProvider` (no exchanger, refused by validation)
- internal/satellite/gcclient/: the only satellite→GC client (D1). Token ZTR, SPIFFE ZTR, sync and credential refresh go through the generated pkg/groundcontrol client, so paths come from the spec and a trailing slash or path prefix in `ground_control_url` works. It refuses an `http://` base URL unless use_unsecure, maps 401/403/429 to `ErrUnauthorized`/`ErrForbidden`/`ErrRateLimited` and keeps only GC's error message, never the raw body. `Credentials`: `RobotBasic` (read per request), `NoCredentials`, `SessionBearer`

### Shared Packages

- internal/shared/env/: All env var parsing for satellite and GC (caarlos0/env struct tags)
- internal/shared/logger/: zerolog logger (context-carried) and audit logger (syslog daemon/network/file, OTLP/HTTP)
- internal/shared/crypto/: AES-GCM provider, Argon2id helpers (m=19 MiB, t=2, p=1)
- internal/shared/spiffe/: SPIFFE Workload API client (X.509 SVID, mTLS HTTP client)
- internal/shared/utils/: Misc utilities (signal context, URL formatting, file I/O)
- internal/shared/edgejournal/: journal record types and versioned payloads shared by satellite and GC
- internal/shared/inventory/: order-independent inventory digest
- internal/shared/policybundle/: edge admission Casbin model (`edge-admission/v1`), bundle format, modes `off|audit|enforce`
- internal/shared/notimpl/: `ErrNotImplemented`, returned only by `fleet.JournalService.Ingest` and the postgres `JournalRepo` (plan item 6.5)
- internal/version/: Version and GitCommit, set via ldflags

### Ground Control Component Structure

- cmd/groundcontrol/main.go: Creates the signal-aware context and executes the unified command
- internal/groundcontrol/cli/root/serve.go: Loads env, logs (but does not fail on) an unreachable Harbor, runs migrations, composes the fleet with `app.NewFleet` through `server.WithFleetFactory`, starts HTTP/TLS/SPIFFE server, the cleanup job, the registry probe and, with `--worker=embedded` (default), the async plane worker; `--worker=off` leaves that to the hidden `groundcontrol worker`
- internal/groundcontrol/server/: Handlers (satellites, groups, configs, auth, users, SPIRE), generated router (`server.gen.go`), per-route security middleware (`routes.go`), cleanup job
- internal/groundcontrol/database/: sqlc-generated PostgreSQL access (do not edit)
- internal/groundcontrol/sql/: `schema/` (goose migrations, also the sqlc schema) and `queries/`
- internal/groundcontrol/migrator/: goose runner; reads `/migrations` when present, otherwise uses the embedded SQL schema so installed binaries work outside the checkout
- internal/groundcontrol/fleet/: Domain types, ports and services (satellites, groups, configs, registration, heartbeats, lifecycle, policy, webhooks, journal); `fleettest` has recording fakes
- internal/groundcontrol/adapters/: `postgres` (sqlc repository, outbox, desired state, aggregate locks, sessions, heartbeats, partitions, identities, rotations, webhooks), `harbor2` (fleet.Registry over the Harbor v2 API and crane, one client per instance), `spire`, `casbin` (embedded model and baseline, poller), `oidc` (go-oidc)
- internal/groundcontrol/authn/: satellite sessions (`hss_` tokens, SHA-256 at rest, positive cache) and OIDC operator login
- internal/groundcontrol/heartbeat/: per-process heartbeat `Buffer` and batched `Flusher`
- internal/groundcontrol/webhook/: CloudEvents, HMAC signer, HTTP sender, dispatcher
- internal/groundcontrol/secrets/: AES-GCM sealer (`DATA_ENCRYPTION_KEY`); internal/groundcontrol/satconfig/: config document validator; internal/groundcontrol/dbpool/: one pool per plane; internal/groundcontrol/advisorylock/: lock keys
- internal/groundcontrol/async/: Outbox publisher, reconcilers, tombstones, registry probe, worker and jobs (stale detector, session and login state expiry, outbox pruner, robot rotator and rotation cache, registry follower)
- internal/groundcontrol/app/: Composition root (`NewFleet`, `NewWorker`)
- internal/groundcontrol/harborhealth/: Startup Harbor health check, logged only; `/health` reports Harbor `degraded` from the registry probe
- internal/groundcontrol/auth/: Password policy validation, Argon2id password hashing (via internal/shared/crypto), session token generation
- internal/groundcontrol/middleware/: Rate limiting (legacy per-IP limiter, `FailureLimiter`, generic token bucket `Limiter`/`LimitBy`), TLS cert watching
- internal/groundcontrol/telemetry/: Prometheus `/metrics` of serve on `METRICS_ADDR` (HTTP requests by route template, heartbeats, heartbeat buffer and flushes, outbox counts read at scrape time, `go_sql_*` per pool); app wires the recorder into the heartbeat sink, the flusher store and `server.WithRequestObserver`
- internal/groundcontrol/spiffe/: SPIFFE provider, middleware, authorizer, embedded SPIRE server, SPIRE server client
- internal/groundcontrol/cli/: `groundcontrol` commands (serve, worker (hidden), auth, get, create, update, delete, register, add, remove, sync, ping, health)
- pkg/groundcontrol/: Generated Go client for the GC API

### Key Concepts

Groups: Collections of artifacts (repository, tag, digest, type) that satellites replicate. GC pushes each group's state as an OCI artifact into Harbor under `satellite/group-state/<group>/state`. A satellite's robot can pull the union of the projects of all its groups plus `satellite`: every membership or group change (register, SPIFFE ZTR robot creation, group sync, add, remove, group delete) goes through `server.syncRobotPermissions`, one `GetProjectsOfSatellites` query and one Harbor update per robot, because Harbor replaces a robot's permissions on each update.

Satellite state: GC republishes `satellite/satellite-state/<name>/state` on every membership or config change, also when the satellite has no groups (then without `states`), so a satellite that leaves its last group stops replicating it (D10, #561).

Configs: Named satellite app configs (intervals, metrics, BYO registry, registry fallback, audit, TLS, peer distribution, etc.) delivered through Harbor as config state artifacts (`satellite/config-state/<config>/state`). Updates use JSON Merge Patch (evanphx/json-patch).

State Replication: The satellite resolves its root state artifact (`satellite/satellite-state/<name>/state`) and each group state with one HEAD per tick and pulls only the ones whose digest changed, using its robot credentials directly against Harbor, then replicates changes into the local store. `replication_concurrency` bounds the groups replicating at once and `replication_bandwidth_bytes_per_sec` caps a Harbor store used only by replication. Every replicated or deleted artifact is appended to the edge journal (`<config dir>/journal`, ADR-0015). GC is not on the replication data path.

OCI Proxy: Unless `--fallback-only`, cmd/satellite always serves a pull-only OCI Distribution endpoint on `127.0.0.1:<proxy-port>` (default 8585, loopback hard-coded) backed by the same local store. `proxy` mode (default) fills a local miss from Harbor (GET replicates into the local store, HEAD only reads metadata); `replica` mode serves only replicated content. Push, delete, tag list, referrers and catalog return UNSUPPORTED. `PROXY_STREAMING` answers a manifest miss once it is stored and streams blobs, with a prefetch lane completing the graph (OCI layout only). `cmd/satellite` wraps the pull handler with `proxy/middleware.Stack(admission.Middleware(...), proxymw.Authenticate(nil), nil)`: non-loopback clients are rejected, edge admission runs from `app_config.admission`, and the metrics slot is nil because the pull handler records itself. A `cache` mode and content-aware policy from ADR-0009 are not implemented.

Registration (ZTR, Zero Touch Registration): Two mutually exclusive paths:
- Token ZTR: Satellite POSTs `{"token": ...}` to `/satellites/ztr` and receives Harbor robot credentials and its state URL. Tokens expire after 24h and are single use: `fleet.RegistrationService.ZeroTouch` claims the token in one statement (`adapters/postgres` tokenRepo), so of concurrent calls exactly one wins, unclaims it if a later step fails so the satellite can retry, and consumes it on success. Until the async plane created the satellite's robot (named `fleet.RobotName`, `<satellite>-<id>`) ZTR answers 503 with `Retry-After`. A consumed token is answered 400 like an unknown one, an expired one 401. Managed in state/registration_process.go.
- SPIFFE ZTR: Satellite presents an X.509 SVID via mTLS to `GET /satellites/spiffe-ztr` and is auto-registered. Managed in state/spiffe_registration.go.

Heartbeat and Status Reporting: The satellite POSTs to `/satellites/sync` (SPIFFE identity, satellite session Bearer, or robot Basic auth while `SATELLITE_ROBOT_AUTH`) with CPU, memory, storage, image count, cached images (BYO only) and CRI activity. `latest_state_digest`, `latest_config_digest`, `last_sync_duration_ms`, `last_replication_error` and `applied_states` come from the last replication run, and a `journal` upload carries edge journal records (ADR-0015). GC buffers beats per process (`heartbeat.Buffer`, flushed every `HEARTBEAT_FLUSH_INTERVAL`). The response can carry events (e.g. `refresh_credentials` when the robot expires within two heartbeat intervals), a renewed session, rotated robot `credentials` and `inventory_requested`. `fleet.JournalService.Ingest` is still `notimpl` (6.5), and `HeartbeatService.Beat` turns that into a 500, so a heartbeat carrying a journal fails until 6.5 lands. Managed in state/reporting_process.go and state/report.go.

Lifecycle and stale detection: `satellite_view.state` is `pending|online|stale|revoked` (ADR-0006 via ADR-0016). The worker's `async.StaleDetector` (every `STALE_CHECK_INTERVAL`) marks an online satellite stale when `last_seen` is older than 3x its `@every` heartbeat interval, else `STALE_THRESHOLD` (15m), never sooner than 2x the flush interval. `/api/satellites/active` and `/stale` list by state, so without a worker nothing becomes stale. Revoke disables the robot through the reconciler; sync and ZTR then answer 403.

BYO Registry: `--byo-registry --registry-url ...` replicates into an external registry instead of the local OCI layout. The proxy then serves from that registry, and it is the only endpoint `--fallback-only` can point CRIs at.

Peer Distribution (ADR-0010, proposed): opt-in `app_config.peer_distribution` block (static_peers, gc_peers, local_groups, reachout_sats group|global, timeout, retries, concurrency) is validated, defaulted when present, and kept across remote config reconciles. With it enabled, `store.PeerFillingStore` races eligible peers (registry endpoints, never the loopback proxy) before Harbor per artifact, first complete graph wins; when the Harbor list fetch fails, `replicateDegraded` replicates the persisted list from peers and skips the Harbor blob copy. GC does not fill `gc_peers`.

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

Satellite (each also has a CLI flag, see Satellite CLI Flags): TOKEN, GROUND_CONTROL_URL, HARBOR_REGISTRY_URL, USE_UNSECURE, SPIFFE_ENABLED, SPIFFE_ENDPOINT_SOCKET, SPIFFE_EXPECTED_SERVER_ID, BYO_REGISTRY, REGISTRY_URL, REGISTRY_USERNAME, REGISTRY_PASSWORD, CONFIG_DIR, REGISTRY_DATA_DIR, DESIRED_STATE_FILE (`<config dir>/desired-state.json`, read only while state.json is empty), SHUTDOWN_TIMEOUT (30s), NO_REGISTRY_FALLBACK, DIRECT_DELIVERY, IMAGE_DIR, PROXY_MODE (proxy|replica, default proxy), PROXY_PORT (8585), STORE_GC_GRACE (10m), PROXY_WRITE_TIMEOUT (60s), PROXY_MAX_CONNECTIONS (0, unlimited), PROXY_STREAMING (false), PROXY_PREFETCH_CONCURRENCY (2). TELEMETRY_ADDR (127.0.0.1:8586, empty disables).

Ground Control:
- Harbor: HARBOR_URL, HARBOR_USERNAME, HARBOR_PASSWORD, ROBOT_DURATION_DAYS (30), SKIP_HARBOR_HEALTH_CHECK (false; only skips the startup log check), HARBOR_RATE_LIMIT (20 requests/s from the worker toward Harbor), HARBOR_PROBE_INTERVAL (30s; Harbor counts as unreachable after three intervals)
- Outbox publisher (worker): OUTBOX_BATCH_SIZE (100), OUTBOX_POLL_INTERVAL (1s), OUTBOX_CONCURRENCY (4), OUTBOX_MAX_ATTEMPTS (10), OUTBOX_BACKOFF_BASE (1s), OUTBOX_BACKOFF_MAX (5m), OUTBOX_LEASE (1m), OUTBOX_RETENTION (168h, `async.OutboxPruner`, keeps rows enabled webhooks have not read)
- Database: DB_HOST, DB_PORT, DB_DATABASE, DB_USERNAME, DB_PASSWORD (escaped by `env.Database.ConnectionURL`), DB_SSLMODE (require; validated, every compose stack and the internal Helm PostgreSQL set disable, chart value `database.sslMode`), DB_SSLROOTCERT. Pools per plane (`dbpool`): DB_URL_API (used as-is, DB_SSLMODE not added; migrations use it), DB_URL_FLEET, DB_URL_ASYNC, and per plane DB_API_*, DB_FLEET_*, DB_ASYNC_* with MAX_OPEN_CONNS, MAX_IDLE_CONNS, CONN_MAX_LIFETIME, CONN_MAX_IDLE_TIME, STATEMENT_TIMEOUT, LOCK_TIMEOUT, IDLE_IN_TX_SESSION_TIMEOUT (timeouts sent as libpq `options`)
- Server and auth: PORT (8080), METRICS_ADDR (:9464, Prometheus /metrics of serve; empty disables), ADMIN_PASSWORD (bootstraps `admin`), SESSION_DURATION (24h), LOCKOUT_DURATION (5m), STALE_THRESHOLD (15m, stale detector), AUTH_FAILURE_LIMIT (20, 0 disables) and AUTH_FAILURE_WINDOW (1m) (failed authentications per client IP before 429), SYNC_MAX_BODY_BYTES (4 MiB, larger sync bodies get 413), GROUP_SYNC_MAX_BODY_BYTES (16 MiB for POST /api/groups/sync) and API_MAX_BODY_BYTES (1 MiB for every other /api body)
- Fleet hot path: SATELLITE_SESSION_TTL (720h), SATELLITE_SESSION_CACHE_TTL (30s, at most 5m), SATELLITE_SESSION_CACHE_SIZE (10000), SATELLITE_ROBOT_AUTH (true), HEARTBEAT_FLUSH_INTERVAL (5s), HEARTBEAT_BUFFER_MAX (10000), HEARTBEAT_MAX_FLUSH_FAILURES (3), STALE_CHECK_INTERVAL (1m), SESSION_EXPIRY_INTERVAL (1h), STATUS_RETENTION_DAYS (7), STATUS_PARTITIONS_AHEAD (3), RATE_LIMIT_SPLIT (true; false: one 10/min per-IP limiter shared by /login, ZTR, SPIFFE ZTR, sync and refresh), RATE_LIMIT_ANONYMOUS (10/1m), RATE_LIMIT_FLEET (3/1s)
- Policy: POLICY_ENGINE (builtin|casbin, default builtin; any other value fails startup), POLICY_POLL_INTERVAL (10s)
- Identity: OIDC_ENABLED (false; discovery at startup, failure stops serve), OIDC_ISSUER_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, OIDC_REDIRECT_URL, OIDC_SCOPES (openid,profile,email,groups), OIDC_USERNAME_CLAIM (preferred_username), OIDC_GROUPS_CLAIM (groups), OIDC_ROLE_MAP (group=role,...), OIDC_DISABLE_PASSWORD_LOGIN (false), OIDC_LOGIN_STATE_TTL (10m), ROBOT_ROTATION_ENABLED (false), ROBOT_ROTATION_BEFORE_EXPIRY (72h), ROBOT_ROTATION_INTERVAL (10m), ROBOT_ROTATION_CACHE_REFRESH (30s), DATA_ENCRYPTION_KEY (base64, 32 bytes; required with rotation or webhooks)
- Webhooks: WEBHOOKS_ENABLED (false), WEBHOOK_BATCH_SIZE (100), WEBHOOK_POLL_INTERVAL (1s), WEBHOOK_BACKOFF_BASE (1s), WEBHOOK_BACKOFF_MAX (5m), WEBHOOK_TIMEOUT (10s), WEBHOOK_DISABLE_AFTER (24h), WEBHOOK_SOURCE (urn:harbor-satellite:ground-control)
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
- Account lockout for LOCKOUT_DURATION after 5 failed attempts for a username; `/login` and `/api` Basic auth share one check (`server.verifyUserCredentials`), so both count toward it and a success through either resets it. `middleware.FailureLimiter` answers 429 for a client IP after AUTH_FAILURE_LIMIT failures across usernames, before any password is verified. `locked_until` is TIMESTAMP and is written in UTC
- Bootstrap: creates `admin` user from ADMIN_PASSWORD on first startup

**Ground Control API Routes** (spec/groundcontrol/openapi.yaml, security wrapping in internal/groundcontrol/server/routes.go):
- Public: `GET /ping`, `GET /health`, `POST /login` (rate limited), `POST /satellites/ztr` (rate limited), `GET /satellites/spiffe-ztr` (SPIFFE required, rate limited), `/oidc/login`, `/oidc/callback`, `/oidc/device`, `/oidc/device/token` (anonymous limiter). Anonymous routes get a 64 KiB `MaxBytesReader`
- Satellite-authenticated: `POST /satellites/sync` and `POST /satellites/refresh` (SPIFFE SVID or robot Basic auth, rate limited; the SPIFFE middleware is the outer layer, so a valid SVID wins over Basic credentials). Refresh rotates the robot secret in Harbor and returns it; an expired robot gets 401 and needs a new ZTR
- Protected (under `/api`): groups, configs, satellites, users, logout
- System admin only: user create/delete and admin password reset, group delete, `/api/spire/*`, `POST /api/satellites/register` (SPIFFE registration), `/api/ops/*`, `/api/policies`, `/api/role-bindings`, `/api/webhooks`. With `POLICY_ENGINE=casbin` `PolicyMiddleware` replaces this check for every `/api` route (baseline in `adapters/casbin/baseline.csv`)
- Background cleanup job: every 24h, in one transaction that takes `pg_try_advisory_xact_lock(advisorylock.Cleanup)` (released at commit or rollback, so it cannot leak through the pool; a replica that does not get it skips the run), deletes status records older than 7 days and orphaned artifacts

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
- `--use-unsecure` / `USE_UNSECURE`: HTTP and skip TLS verification; required for an `http://` Ground Control URL (every GC call, ZTR included, goes through `internal/satellite/gcclient`)
- `--mirrors`: CRI mirror config (`CRI:registry1,registry2`), no env var
- `--spiffe-enabled`, `--spiffe-endpoint-socket`, `--spiffe-expected-server-id` / `SPIFFE_*`
- `--byo-registry`, `--registry-url`, `--registry-username`, `--registry-password` / `BYO_REGISTRY`, `REGISTRY_*`
- `--config-dir` / `CONFIG_DIR`, `--registry-data-dir` / `REGISTRY_DATA_DIR`, `--desired-state-file` / `DESIRED_STATE_FILE`
- `--shutdown-timeout` / `SHUTDOWN_TIMEOUT` (30s)
- `--no-registry-fallback` / `NO_REGISTRY_FALLBACK`
- `--fallback-only`: Apply CRI fallback configs and exit, no env var; rejected together with an explicit `--proxy-mode` / `PROXY_MODE`
- `--proxy-mode` / `PROXY_MODE` (proxy|replica), `--proxy-port` / `PROXY_PORT` (8585)
- `--proxy-write-timeout` / `PROXY_WRITE_TIMEOUT` (60s): per-write deadline (`proxy.WriteDeadlineHandler`), not a server `WriteTimeout`, so long blob downloads that make progress are not cut; `--proxy-max-connections` / `PROXY_MAX_CONNECTIONS` (0, unlimited) wraps the listener in `netutil.LimitListener`
- `--proxy-streaming` / `PROXY_STREAMING` (false), `--proxy-prefetch-concurrency` / `PROXY_PREFETCH_CONCURRENCY` (2): streaming pull-through in proxy mode with the OCI layout (ignored with a BYO registry, with a warning)
- `--telemetry-addr` / `TELEMETRY_ADDR` (127.0.0.1:8586): `/healthz` (checks `store` and `proxy`, 503 naming the failing one) and Prometheus `/metrics` (`satellite_proxy_pull_duration_seconds`, `satellite_proxy_pull_through_fills_total`, `satellite_replication_bytes_total`, `satellite_replication_duration_seconds`, `satellite_store_lock_wait_seconds`, Go and process collectors); a taken port fails startup
- `--store-gc-grace` / `STORE_GC_GRACE` (10m): unreferenced OCI layout content is deleted only after this long; a collector runs at this interval and `Delete` collects too (0 deletes at once, no collector)
- `--direct-delivery` / `DIRECT_DELIVERY`, `--image-dir` / `IMAGE_DIR`: experimental k3s/RKE2 tarball delivery

### Container Runtime Integration

CRI configs make the runtime pull through the satellite with the upstream as fallback (containerd: `hosts.toml` with the upstream as `server` and the endpoint as a `pull`/`resolve` host). In a normal run the endpoint is the OCI proxy at `127.0.0.1:<proxy-port>`. With `--fallback-only` no proxy runs, so the endpoint is the BYO registry URL; without `--byo-registry` it prints a warning and skips. Priority: GC-delivered `registry_fallback` > `--mirrors` > `--no-registry-fallback`.
- Format: `--mirrors=<CRI>:<registry1>,<registry2>`
- Example: `--mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io`
- Docker only supports mirroring docker.io, use `--mirrors=docker:true`
- Requires write access to CRI config files (usually root); Docker needs a daemon restart

### Database Migrations

goose migrations live in `internal/groundcontrol/sql/schema/` (001 to 024, no 014; 024 partitions `satellite_status` by day) and run automatically on GC startup, after a check that refuses PostgreSQL below 19 and `wal_level=minimal` (ADR-0020, `migrator/server.go`). The Dockerfile copies them to `/migrations`.

### Hot Reload

The satellite watches `config.json`. Log level, state replication interval and audit settings apply without restart; other changes need a restart.

## Testing

- Unit tests colocated with source files (*_test.go)
- E2E tests are Taskfile-driven flows (taskfiles/e2e.yml), not Go tests; `verify` pulls one image through the proxy (`verify-proxy-pull`) and waits for two heartbeats with `go run ./test/e2e/cmd/verify-heartbeat`. `HARBOR_EXTERNAL=1 HARBOR_NETWORK=<net> HARBOR_HOST_URL=<url>` runs them against a running Harbor
- E2E compose stacks: `test/e2e/docker/docker-compose.yml`, `test/e2e/docker/docker-compose.spiffe.yml`
- Test configs in test/e2e/testconfig/, fixtures in test/e2e/testdata/
- GC handler tests use go-sqlmock. Integration tests in the same packages run against real Postgres when `TEST_DATABASE_URL` is set and skip otherwise; `task test:integration` starts `test/integration/docker-compose.yml` (postgres:19rc1 with pg_stat_statements, host port `TEST_PG_PORT`, default 55434) and sets it for `./internal/groundcontrol/...` and `./test/contract/...`. `test/contract` `TestSatelliteJournalReachesGroundControl` composes Ground Control as `serve --worker=embedded` does and drives the satellite's ZTR, journal and heartbeat code against it
- `internal/shared/logger/logtest.Capture(t)` routes the context-carried zerolog logger into a buffer at every level; tests use it to assert secrets never reach the log
- test/arch/: repository-wide tests. `TestNoSecretsInLogArguments` (D4) parses every non-test file in cmd, internal and pkg and fails on a log argument named like a password, secret or token, or a logged response body; never log a GC or Harbor response body
- `internal/groundcontrol/dbtest`: a fresh database per test with all goose migrations, plus pg_stat_statements counts per test database. `internal/groundcontrol/harbortest`: in-process Harbor API and registry stub that records every call and can fail, go down or hold requests
- `internal/groundcontrol/server`: SQL statement and Harbor call budgets (`budget_test.go`), golden responses of every endpoint in `testdata/golden` (refresh with `go test ./internal/groundcontrol/server -run TestGoldenAPI -update`). `test/contract`: the paths the satellite calls must be served by the GC router
- Tests for Harbor 3 plan items that have not landed start with `pending.Plan` (`internal/shared/testutil/pending`) and skip unless `HARBOR3_PENDING=1`; the implementing change removes the marker. Left: one 6.5 statement budget test (`internal/groundcontrol/server/hot_path_test.go`)

## CI/CD

GitHub Actions workflows:
- .github/workflows/test.yaml (PRs): unit tests + Codecov; Ground Control database generation drift check (`task generate:groundcontrol-database`); Ground Control integration tests via `task test:integration`; govulncheck, GoReleaser snapshot, satellite and GC builds, E2E main, BYO, SPIFFE, multi-group, and a matrix of crash recovery (both stores), Harbor outage and lifecycle (all ubuntu-latest)
- .github/workflows/lint.yaml (PRs): golangci-lint v2.12.2 binary on ubuntu-latest, called directly rather than through `task lint-report`; job `arch` runs `task lint:arch`
- .github/workflows/release.yaml (push to main and `v*.*.*` tags, ubuntu-latest): per-arch builds (linux amd64/arm64/ppc64le/s390x/riscv64) of the `satellite` and `ground-control` images to `${REGISTRY_ADDRESS}/${PROJECT_NAME}` from repository variables (README: registry.goharbor.io/harbor-satellite), multi-arch manifest, Cosign keyless signing; tags additionally run `task release` (GoReleaser)
- .github/workflows/labeler.yaml (`pull_request_target`, ubuntu-latest): auto-labels PRs (documentation for *.md, golang for *.go)

Key tasks: `build`, `build-all`, `lint`, `lint:arch`, `lint-report`, `vuln`, `vuln-report`, `test:integration`, `e2e*`, `generate:groundcontrol`, `byo-up`, `byo-down`, `publish`, `publish-and-sign`, `release`, `snapshot`, `clean`.

## Architecture Decisions

ADRs in docs/decisions/:
- ADR-0001: Skopeo vs Crane (accepted, chose Crane; ADR-0009 supersedes the target state)
- ADR-0002: Zot vs Docker Registry (superseded, Zot replaced by ORAS in #648)
- ADR-0003: Remote config injection (accepted, implemented with a different API than its examples)
- ADR-0004: Ground Control authentication (accepted; session-based auth with bearer tokens; partly superseded by ADR-0016 and ADR-0018)
- PDR-0005: SPIFFE identity and security (accepted)
- ADR-0006: Satellite lifecycle states (accepted; implemented through ADR-0016 in `satellite_view.state`)
- ADR-0007, ADR-0008: PARSEC hardware-backed identity and bootstrapping flow (deprecated; PARSEC code was removed in #526). Related proposal in docs/proposals/
- ADR-0009: ORAS OCI storage and a policy-enforcing transparent proxy (proposed; ORAS store #648, proxy #649/#654, request-level edge admission through ADR-0017; `cache` mode and content-aware policy not implemented)
- ADR-0010: Peer-to-peer distribution between satellites (proposed; config #674, roster #676, peer-first replication #684/#697, degraded replication #687/#701; GC does not fill gc_peers)
- ADR-0013: Edge store concurrency and pull-through streaming (accepted; implemented)
- ADR-0014: Ground Control async plane, transactional outbox and level-triggered publisher (accepted; implemented)
- ADR-0015: Edge journal and upload protocol (accepted; satellite side implemented, GC ingest 6.5 not)
- ADR-0016: Satellite sessions and the fleet hot path (accepted; implemented; supersedes the satellite part of ADR-0004)
- ADR-0017: Policy engine, Casbin for Ground Control and edge admission (accepted; implemented; supersedes ADR-0009's CEL section)
- ADR-0018: Operator identity through OIDC and satellite credential rotation (accepted; OIDC and rotation implemented, keyless pulls wait for Harbor 3)
- ADR-0019: Ground Control hosting and extension points (accepted; webhooks, event catalog, proxy middleware implemented; registry event adapter, delivery-lag metric and PostgreSQL 19 outbox not)
- ADR-0020: PostgreSQL 19 as the minimum version (accepted; startup check in `migrator/server.go`, all stacks on postgres:19rc1 until 19.0; logical-slot dispatcher not implemented; supersedes ADR-0014 decision 10)
- groundcontrol-internal-package-migration.md: moving GC into the root module (done)

## Common Workflows

### Adding a new API endpoint to Ground Control

1. Add the path and schemas to spec/groundcontrol/openapi.yaml
2. Run `task generate:groundcontrol` (server and client)
3. Implement the generated interface method in internal/groundcontrol/server/*_handlers.go
4. If the route needs non-default auth (public, satellite, system admin), update routeSecurityMiddleware / requiresSystemAdmin in routes.go
5. Add SQL in internal/groundcontrol/sql/queries (and a migration in sql/schema if needed), then `sqlc generate`
6. Add a CLI command under internal/groundcontrol/cli/root if operators need it
7. Update docs/reference/api.md (and docs/reference/ground-control.md for new env vars)

### Adding a new satellite feature

1. Implement in the appropriate internal/satellite/ package
2. Update pkg/config/ if configuration changes are needed, with a modifier in modifiers.go
3. Add validation and defaults in pkg/config/validate.go
4. Update cmd/satellite/main.go and internal/shared/env/harbor-satellite.go for new flags and env vars
5. Update examples/config.example.json, .env.example and docs/reference/satellite.md

### Modifying state replication logic

State replication in internal/satellite/state/ and internal/satellite/store/:
- fetcher.go: Fetching state artifacts from Harbor
- state_process.go: Orchestrating fetch, diff, replicate, config reconcile, direct delivery
- apply_changes.go: `deleteRetired` deletes what groups dropped or removed groups held, after every group replicated, minus the union of all groups' entities (D12); pending deletes persist in `state.json` `retired`
- store/store.go: Backend-neutral `Store` contract shared with the OCI proxy
- store/oci.go: Replicating OCI graphs into the local image layout (ORAS)
- store/registry.go: Remote registry store (ORAS) for Harbor and BYO registries
- proxy/process/image/pull.go: Proxy pull path; proxy-mode misses replicate through the same `Store.Replicate`
- direct_delivery.go: k3s/RKE2 tarball delivery
- registration_process.go, spiffe_registration.go: Token and SPIFFE ZTR
- reporting_process.go, report.go, catalog.go: Heartbeat, metrics, cached image list (BYO only)
- state_persistence.go: Atomic persistence to state.json
- helpers.go: URL validation, state fetcher creation

Each group's tick replicates first and deletes afterwards (`state.applyGroupChanges`), so a re-tagged image never goes missing and shared layers are not fetched again. Removals that a current entity of the group still references by image and digest are dropped (`state.withoutWanted`). `Store.Delete` is digest guarded: `OCIStore` untags only while the tag still resolves to the artifact's digest and always runs GC afterwards; `RegistryStore` deletes the artifact's own digest.

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

## Harbor 3 architecture (plan TAS-644)

The 1.0 work implemented the Harbor 3 plan (`plan.md` in the primary checkout, ADRs 0013 to 0020). User-facing docs: `docs/architecture.md`, `docs/reference/`, `docs/operations/`, `CHANGELOG.md`. Notes for code changes:

- Architecture rules (0.1): `.go-arch-lint.yml`, checked by `task lint:arch` (go-arch-lint pinned by `GO_ARCH_LINT_VERSION`, run with `go run`) before golangci-lint and as lint job `arch`. Every package must belong to a component and a component may import its own packages only when it lists itself, so a new package needs an entry; test helper packages named `*test` fall into `testsupport`. No exceptions (`TestArchLintHasNoExceptions`): the proxy mode type is `config.ProxyMode` and `cmd/satellite` injects the device-bound encryptor with `config.WithEncryptor`; without one, `encrypt_config` writes fail and a sealed `config.json` is refused (`config.ErrNoEncryptor`)
- Composition: `internal/groundcontrol/app` builds everything from env (`NewFleet` for serve: services, sessions, heartbeat buffer and flusher, Casbin policy, rotation cache, OIDC login, probe; `NewWorker`: publisher, reconcilers, tombstones, stale detector, partition maintainer, session and login state expiry, outbox pruner, rotator, webhook dispatcher). `cli/root/compose.go` wires them into `serve`; `cli/root/worker.go` passes no SPIRE client, so a standalone worker's tombstones leave SPIRE entries. The server never imports the async plane
- Requests: admin handlers call `fleet.Services` and commit one transaction with its outbox event; only ZTR and refresh call Harbor in the request. The satellite routes always take the session path (`authenticateSatellite`): SVID, then Bearer session, then robot Basic while `SATELLITE_ROBOT_AUTH`
- Async plane: claims with `FOR UPDATE SKIP LOCKED` under a lease and a per-aggregate session advisory lock, releases on registry outages, pauses while the probe is stale and re-arms dead events on recovery. Reconcilers publish only when the rendered digest differs from `desired_state.published_digest`
- Pools (3.2, 3.3): `dbpool` opens API, fleet, async; `Server.fleet()` serves sync, refresh and satellite auth, `Server.background()` the cleanup job. lib/pq stays: it cancels server queries on context end (`TestLibPQCancelsQueryOnContextCancel`)
- Schema (3.4, 3.5, 4.5): migration 023 (robot uniqueness, RESTRICT, heartbeat CHECK, `configs.version` with `UpdateConfigIfVersion`, TIMESTAMPTZ via `AT TIME ZONE 'UTC'`), 024 (daily `satellite_status` partitions, `PartitionMaintainer`). Queries comparing with the app clock cast `sqlc.arg(now)::TIMESTAMPTZ`. Every migration reverts to 0 (`TestMigrations_UpDownUp`)
- Edge store (5): `store.OpenOCIStore` with `AtomicIndex` and `RecoverIndex` (torn index moved to `index.json.torn-<time>`, `Satellite.RequireFullResync`); `OCIStore.mu` is the index lock (shared for resolve and open, exclusive for publish, untag, collect); an open `Fetch` body holds a `RefCounter` reference; `CollectGarbage` with `FillLeases` roots and `GCGrace`; replication stages copies in `.stage/`; streaming fills (`StreamBlob`, `Filler.FillManifest`, `NewLanePrefetcher`) only with `PROXY_STREAMING`
- Edge cold path (6): `state.StateArtifactChanged` HEADs before pulling, `replication_concurrency`, `replication_bandwidth_bytes_per_sec` via `SetReplicationSource` and `lane.ThrottledTransport`, `state.RunSummary` feeds `applied_states`, `latest_*_digest`, `last_sync_duration_ms` and `last_replication_error`. Journal uploads one record until GC answered once, then `journal.upload_batch`
- Open items: 6.5 GC journal ingest (`fleet.JournalService.Ingest` and postgres `JournalRepo` return `notimpl`; a heartbeat with a journal gets 500), the PostgreSQL 19 startup check (ADR-0020), a Harbor 3 adapter for `fleet.RegistryEventSource` and a token exchanger for `registryauth.WorkloadIdentityProvider`
- Policy (7): `PolicyRoutes` maps every `/api` route to a resource type and action (`TestPolicyRoutes_CoverGeneratedRouter`); `PolicyMiddleware` is used only with `POLICY_ENGINE=casbin`. Config writes are validated by `satconfig.Validator` (422). Edge admission: `policybundle` model `edge-admission/v1`, evaluator cached per bundle digest
- Identity (8): `UpsertOIDCUser` never adopts a local account, `TakeOIDCLoginState` is single use and bound to the `gc_oidc_binding` cookie; rotation stages a successor robot sealed with `secrets.AESGCM`, delivers it in the heartbeat and deletes the old robot after the satellite acknowledged its generation
- Extension points (9): webhook dispatcher reads the outbox with the xmin guard (`ReadOutboxAfter`), checkpoints never move backwards, a failing subscription is disabled after `WEBHOOK_DISABLE_AFTER`; event catalog `fleet.EventPayloads` and `spec/events/v1` (checked by `test/contract`); `async.RegistryFollower` and `GroupService.FollowPush` exist, nothing composes them
