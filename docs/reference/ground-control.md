# Ground Control Configuration Reference

Ground Control is configured through environment variables. The `groundcontrol` executable also loads a `.env` file from the working directory when one exists. A value that does not parse (a bad duration, an unknown `DB_SSLMODE`, `POLICY_ENGINE` or `OIDC_ROLE_MAP` role) stops startup with an error that names the variable.

## Processes

| Command | What it runs |
|---|---|
| `groundcontrol serve` | Migrations, the HTTP API, the cleanup job, the Harbor probe, the heartbeat flusher, the policy poller and, with `--worker=embedded` (default), the async worker. |
| `groundcontrol serve --worker=off` | The same without the async worker. Run `groundcontrol worker` next to it. |
| `groundcontrol worker` | Migrations and the async worker only: outbox publisher, Harbor reconcilers, stale detector, session and login state expiry, status partition maintenance, outbox retention, robot rotation and webhook delivery when enabled. Hidden from `--help`. Several workers may run against one database. |

Without a worker, admin changes are stored but never reach Harbor, token registration answers `503` until the satellite's robot exists, and no satellite is marked stale. A standalone `worker` connects to no SPIRE server, so deleting a satellite leaves its SPIRE entry; run the worker embedded when you use SPIRE.

## Harbor

| Variable | Default | Description |
|---|---|---|
| `HARBOR_URL` | | Harbor base URL. An `http://` URL disables TLS toward Harbor. |
| `HARBOR_USERNAME`, `HARBOR_PASSWORD` | | An account that can create projects and robot accounts. |
| `ROBOT_DURATION_DAYS` | `30` | Lifetime of satellite robot accounts. Zero, negative or invalid values fall back to 30. |
| `HARBOR_RATE_LIMIT` | `20` | Requests per second the worker sends to Harbor. |
| `HARBOR_PROBE_INTERVAL` | `30s` | How often Ground Control pings Harbor. After three intervals without contact `/health` reports Harbor `degraded` and the outbox publisher pauses. |
| `SKIP_HARBOR_HEALTH_CHECK` | `false` | Skips the Harbor check at startup. That check only logs, so this only silences the log line. |

## Database

| Variable | Default | Description |
|---|---|---|
| `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD` | | Connection settings. Credentials are escaped in the URL, so any characters work. |
| `DB_SSLMODE` | `require` | `disable`, `allow`, `prefer`, `require`, `verify-ca` or `verify-full`. A server without TLS needs `disable`. |
| `DB_SSLROOTCERT` | | CA file for `verify-ca` and `verify-full`. |
| `DB_URL_API` | | Complete URL of the API pool, replacing the settings above. Migrations use it. `DB_SSLMODE` is not added to it. |
| `DB_URL_FLEET` | | URL of the fleet pool (heartbeats, credential refresh, satellite authentication). |
| `DB_URL_ASYNC` | | URL of the async pool (worker, cleanup). |

Pool settings, with `<PLANE>` one of `API`, `FLEET`, `ASYNC`. Zero keeps the Go `database/sql` default. Setting any of them for `FLEET` or `ASYNC` without a URL opens a separate pool on the API URL; with none set, that plane shares the API pool.

| Variable | Description |
|---|---|
| `DB_<PLANE>_MAX_OPEN_CONNS` | Maximum open connections. |
| `DB_<PLANE>_MAX_IDLE_CONNS` | Maximum idle connections. |
| `DB_<PLANE>_CONN_MAX_LIFETIME` | Maximum connection age. |
| `DB_<PLANE>_CONN_MAX_IDLE_TIME` | Maximum idle time of a connection. |
| `DB_<PLANE>_STATEMENT_TIMEOUT` | `statement_timeout` of the plane's connections. |
| `DB_<PLANE>_LOCK_TIMEOUT` | `lock_timeout`. |
| `DB_<PLANE>_IDLE_IN_TX_SESSION_TIMEOUT` | `idle_in_transaction_session_timeout`. |

Every connection sets `application_name` to `groundcontrol-api`, `groundcontrol-fleet` or `groundcontrol-async`. See [Database Operations](../operations/database.md) for roles per plane.

## Server and Operator Authentication

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Listen port. |
| `ADMIN_PASSWORD` | | Creates the `admin` system administrator on first start. Required then, and must pass the password policy. |
| `SESSION_DURATION` | `24h` | Lifetime of an operator session from `/login` or OIDC. |
| `LOCKOUT_DURATION` | `5m` | Lockout after 5 failed logins for one username, through `/login` or Basic auth. |
| `AUTH_FAILURE_LIMIT` | `20` | Failed authentications per client IP, across usernames, before `429`. `0` disables it. |
| `AUTH_FAILURE_WINDOW` | `1m` | Window of `AUTH_FAILURE_LIMIT`. |
| `PASSWORD_MIN_LENGTH` | `8` | |
| `PASSWORD_MAX_LENGTH` | `128` | |
| `PASSWORD_REQUIRE_UPPERCASE` | `true` | |
| `PASSWORD_REQUIRE_LOWERCASE` | `true` | |
| `PASSWORD_REQUIRE_NUMBER` | `true` | |
| `PASSWORD_REQUIRE_SPECIAL` | `false` | |

## Rate Limits

| Variable | Default | Description |
|---|---|---|
| `RATE_LIMIT_SPLIT` | `false` | Off: one limiter of 10 requests per minute per client IP is shared by `/login`, `/satellites/ztr`, `/satellites/spiffe-ztr`, `/satellites/sync` and `/satellites/refresh`. On: anonymous routes are limited per client IP and satellite routes per authenticated satellite. |
| `RATE_LIMIT_ANONYMOUS` | `10/1m` | With the split: requests per client IP on ZTR and the OIDC routes, as `<requests>/<duration>`. |
| `RATE_LIMIT_FLEET` | `3/1s` | With the split: requests per satellite on sync and refresh. |

Without the split, satellites that share one source address, behind NAT or behind a reverse proxy, share 10 requests per minute. A fleet of more than a few satellites needs `RATE_LIMIT_SPLIT=true`. The shared limiter always keys on the TCP peer address. The split limiters and the failure limit use `X-Forwarded-For` or `X-Real-IP` when `AUDIT_TRUST_FORWARDED_HEADERS=true`; enable it only behind a proxy that sets those headers.

## Satellite Sessions and Fleet Jobs

| Variable | Default | Description |
|---|---|---|
| `SATELLITE_SESSION_TTL` | `720h` | Lifetime of a satellite session. It is renewed in the last third of its lifetime. |
| `SATELLITE_SESSION_CACHE_TTL` | `30s` | How long a verified session stays cached per process, at most `5m`. A revocation takes up to this long to reach every process. |
| `SATELLITE_SESSION_CACHE_SIZE` | `10000` | Cached sessions per process. |
| `SATELLITE_ROBOT_AUTH` | `true` | Accept robot Basic auth on satellite routes next to sessions and SVIDs. Satellites registered before sessions existed need it until their next heartbeat gives them a session. |
| `HEARTBEAT_FLUSH_INTERVAL` | `5s` | Heartbeats are buffered per process and written in batches at this interval. |
| `HEARTBEAT_BUFFER_MAX` | `10000` | Buffered heartbeats per process. |
| `HEARTBEAT_MAX_FLUSH_FAILURES` | `3` | After a failed batch, satellites are flushed one by one; a satellite whose beats fail this often is dropped from the buffer with an error log. |
| `STALE_THRESHOLD` | `15m` | Stale threshold for a satellite whose heartbeat interval is not `@every`. Others are stale after three intervals, never sooner than twice the flush interval. |
| `STALE_CHECK_INTERVAL` | `1m` | How often the worker marks satellites stale. |
| `SESSION_EXPIRY_INTERVAL` | `1h` | How often the worker deletes expired satellite sessions and OIDC login states. |
| `STATUS_RETENTION_DAYS` | `7` | Daily `satellite_status` partitions older than this are dropped. |
| `STATUS_PARTITIONS_AHEAD` | `3` | Partitions created ahead of time. |
| `SYNC_MAX_BODY_BYTES` | `4194304` | Parsed, not enforced in this release. |

## Async Plane

| Variable | Default | Description |
|---|---|---|
| `OUTBOX_BATCH_SIZE` | `100` | Events claimed per poll. |
| `OUTBOX_POLL_INTERVAL` | `1s` | Poll interval. |
| `OUTBOX_CONCURRENCY` | `4` | Events handled at once per worker. |
| `OUTBOX_MAX_ATTEMPTS` | `10` | Attempts before an event stays failed. Failed events are listed by `GET /api/ops/outbox?state=failed` and re-armed when Harbor becomes reachable again. |
| `OUTBOX_BACKOFF_BASE`, `OUTBOX_BACKOFF_MAX` | `1s`, `5m` | Exponential retry backoff. |
| `OUTBOX_LEASE` | `1m` | How long a claimed event stays leased to one worker. |
| `OUTBOX_RETENTION` | `168h` | Done events older than this are deleted, except those an enabled webhook subscription has not read yet. |

## Policy

| Variable | Default | Description |
|---|---|---|
| `POLICY_ENGINE` | `builtin` | `builtin`: system administrators may do everything, admins everything except the system-admin routes. `casbin`: every `/api` request is checked against the Casbin baseline plus rules stored through `/api/policies` and `/api/role-bindings`. |
| `POLICY_POLL_INTERVAL` | `10s` | How often each process reloads stored rules with `casbin`. |

## OIDC Operator Login

| Variable | Default | Description |
|---|---|---|
| `OIDC_ENABLED` | `false` | Enable `/oidc/login`, `/oidc/callback` and the device flow. The provider is discovered at startup; an unreachable issuer stops startup. |
| `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_REDIRECT_URL` | | Provider client. The redirect URL is `<gc>/oidc/callback`. |
| `OIDC_SCOPES` | `openid,profile,email,groups` | |
| `OIDC_USERNAME_CLAIM` | `preferred_username` | |
| `OIDC_GROUPS_CLAIM` | `groups` | |
| `OIDC_ROLE_MAP` | | `group=role,...` with role `admin` or `system_admin`. A user whose groups map to no role cannot log in. |
| `OIDC_LOGIN_STATE_TTL` | `10m` | Lifetime of a pending browser login. |
| `OIDC_DISABLE_PASSWORD_LOGIN` | `false` | Refuse password login for every user except the bootstrap `admin`. |

The browser flow sets a `Secure` cookie, so Ground Control must be served over HTTPS for it. `groundcontrol auth login --oidc` uses the device flow.

## Robot Rotation, Webhooks and Secrets

| Variable | Default | Description |
|---|---|---|
| `DATA_ENCRYPTION_KEY` | | Base64 of 32 bytes (`openssl rand -base64 32`). Seals staged robot secrets and webhook secrets with AES-256-GCM. Required when either feature is enabled. |
| `ROBOT_ROTATION_ENABLED` | `false` | Rotate satellite robots before they expire. See [Keyless Pulls and Robot Rotation](../guides/keyless-pulls.md). |
| `ROBOT_ROTATION_BEFORE_EXPIRY` | `72h` | Rotate robots expiring within this window. |
| `ROBOT_ROTATION_INTERVAL` | `10m` | How often the worker looks for robots to rotate. |
| `ROBOT_ROTATION_CACHE_REFRESH` | `30s` | How often each serve process reloads pending rotations. |
| `WEBHOOKS_ENABLED` | `false` | Enable `/api/webhooks` and delivery. |
| `WEBHOOK_BATCH_SIZE` | `100` | Events read per poll and subscription. |
| `WEBHOOK_POLL_INTERVAL` | `1s` | |
| `WEBHOOK_BACKOFF_BASE`, `WEBHOOK_BACKOFF_MAX` | `1s`, `5m` | Retry backoff of a failing receiver. |
| `WEBHOOK_TIMEOUT` | `10s` | Per delivery. |
| `WEBHOOK_DISABLE_AFTER` | `24h` | A subscription that fails this long is disabled. |
| `WEBHOOK_SOURCE` | `urn:harbor-satellite:ground-control` | CloudEvents `source`. |

## TLS, SPIFFE and SPIRE

| Variable | Default | Description |
|---|---|---|
| `TLS_CERT_FILE`, `TLS_KEY_FILE` | | Both set: serve HTTPS (TLS 1.2 minimum). The files are checked for changes every 30 seconds and reloaded. |
| `TLS_CA_FILE` | | Require and verify client certificates against this CA. |
| `SPIFFE_SERVER_ENABLED` | `false` | Serve mTLS with an X.509 SVID. Takes priority over `TLS_*`. |
| `SPIFFE_TRUST_DOMAIN` | `harbor-satellite.local` | |
| `SPIFFE_PROVIDER` | `sidecar` | `sidecar` (Workload API) or `static` (files). |
| `SPIFFE_ENDPOINT_SOCKET` | `unix:///run/spire/sockets/agent.sock` | Workload API socket for `sidecar`. |
| `SPIFFE_CERT_FILE`, `SPIFFE_KEY_FILE`, `SPIFFE_BUNDLE_FILE` | | Files for `static`. |
| `EMBEDDED_SPIRE_ENABLED` | `false` | Start a `spire-server` binary inside Ground Control (join token attestation only, in-memory key manager, port 8081). |
| `SPIRE_DATA_DIR` | `/tmp/spire-data` | Data directory of the embedded server. |
| `SPIRE_TRUST_DOMAIN` | `harbor-satellite.local` | |
| `SPIRE_BIND_ADDRESS` | `127.0.0.1` | |
| `SPIRE_SERVER_SOCKET` | | Admin socket of an external SPIRE server, used when the embedded one is off. |
| `SPIRE_SERVER_ADDRESS`, `SPIRE_SERVER_PORT` | `spire-server`, `8081` | External server address handed to satellites in join-token registration. |

`SPIFFE_SERVER_ENABLED` is Ground Control's variable. `SPIFFE_ENABLED` belongs to the satellite.

## Audit

`AUDIT_LOG_ENABLED`, `AUDIT_SYSLOG_*`, `AUDIT_OTEL_ENDPOINT` and `AUDIT_TRUST_FORWARDED_HEADERS` are described in [Audit Logging](../guides/audit-logging.md).

## CLI Client

The same executable is the administration client. Flags: `--server` (default `https://localhost:8080/`), `--token`, `--timeout` (`30s`), `--insecure`, `--config`. Environment variables use the `GROUND_CONTROL_` prefix (`GROUND_CONTROL_URL`, `GROUND_CONTROL_TOKEN`). Passwords are read only from `GROUND_CONTROL_PASSWORD`, `GROUND_CONTROL_USER_PASSWORD`, `GROUND_CONTROL_CURRENT_PASSWORD` and `GROUND_CONTROL_NEW_PASSWORD`.

The CLI covers satellites, groups, configs, users, SPIRE and sessions. Revocation, lifecycle, policies, role bindings, webhooks and the outbox are API only in this release.
