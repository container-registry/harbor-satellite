# Changelog

All notable changes to Harbor Satellite are recorded here. Versions follow [Semantic Versioning](https://semver.org/).

## [1.0.0] - Unreleased

1.0 is the first release with a stable configuration, API and on-disk format. Upgrade notes and every breaking change are in [Upgrading to 1.0](docs/operations/upgrading.md#upgrading-to-10-from-00x).

### Breaking Changes

- Ground Control: `DB_SSLMODE` defaults to `require`. A PostgreSQL without TLS needs `DB_SSLMODE=disable`.
- Ground Control: migration 023 adds `UNIQUE (satellite_id)` to `robot_accounts` and stops on duplicates, makes configs in use undeletable, adds optimistic config versions and converts timestamps to `TIMESTAMPTZ`, reading them as UTC.
- Ground Control: migration 024 partitions `satellite_status` by day. Status older than `STATUS_RETENTION_DAYS` (7) is dropped.
- Ground Control: admin changes reach Harbor through the async worker. Token registration answers `503` with `Retry-After` until the robot exists, and a used token answers `400`.
- Ground Control: concurrent config updates answer `409`, invalid config documents `422`.
- Ground Control: `/api` Basic auth counts toward the account lockout, and failed authentications per client IP are limited (`AUTH_FAILURE_LIMIT`).
- Ground Control: `RATE_LIMIT_SPLIT` defaults to `true`. Sync and refresh are limited per satellite (`RATE_LIMIT_FLEET`, 3/1s), ZTR and OIDC per client IP (`RATE_LIMIT_ANONYMOUS`, 10/1m), and `/login` only by `AUTH_FAILURE_LIMIT` and the lockout, instead of one limiter of 10 requests per minute per IP for all of them. A satellite that sends more than 3 requests per second now gets `429`. `RATE_LIMIT_SPLIT=false` restores the old limiter.
- Satellite: an `http://` Ground Control URL needs `use_unsecure`.
- Satellite: `/healthz` and `/metrics` listen on `127.0.0.1:8586` by default; a port in use stops startup.
- Satellite: unreferenced local content is collected after `STORE_GC_GRACE` (10 minutes).

### Added

- Satellite: pull-only OCI proxy on `127.0.0.1:8585` in `proxy` and `replica` modes, used as the container runtime mirror endpoint.
- Satellite: streaming pull-through (`PROXY_STREAMING`), with background graph completion.
- Satellite: proxy write deadline and connection cap (`PROXY_WRITE_TIMEOUT`, `PROXY_MAX_CONNECTIONS`).
- Satellite: `/healthz` and Prometheus `/metrics` for pulls, pull-through fills, replication and index lock waits.
- Satellite: crash-safe OCI layout: atomic `index.json`, recovery of a torn index with full resync, mark-and-sweep garbage collection that respects open readers.
- Satellite: edge journal of replication results, CRI results and pull-through fills, uploaded with heartbeats.
- Satellite: replication concurrency and bandwidth limits (`replication_concurrency`, `replication_bandwidth_bytes_per_sec`); state artifacts are fetched only when their digest changed.
- Satellite: peer distribution: copy artifacts from other satellites' registries before Harbor, and replicate the last known state from peers while Harbor is unreachable.
- Satellite: edge admission policy (`app_config.admission`, `audit` or `enforce`) evaluated on every proxy request.
- Satellite: bootstrap desired state file (`DESIRED_STATE_FILE`).
- Ground Control: transactional outbox and level-triggered reconcilers; `serve --worker` and `groundcontrol worker`; `GET /api/ops/outbox`.
- Ground Control: starts while Harbor is down; `/health` reports Harbor `degraded`.
- Ground Control: satellite sessions, buffered heartbeats, lifecycle states (`pending`, `online`, `stale`, `revoked`), `revoke`, `unrevoke` and `lifecycle` endpoints, convergence reporting.
- Ground Control: one database pool per plane with timeouts (`DB_URL_*`, `DB_<PLANE>_*`), `DB_SSLMODE` and `DB_SSLROOTCERT`.
- Ground Control: per-satellite and per-IP rate limits (`RATE_LIMIT_SPLIT`).
- Ground Control: Casbin policy engine with stored rules and role bindings (`POLICY_ENGINE=casbin`, `/api/policies`, `/api/role-bindings`).
- Ground Control: OIDC operator login (browser and device flow, `groundcontrol auth login --oidc`), group to role mapping, password login limited to the bootstrap admin.
- Ground Control: Harbor robot rotation before expiry, delivered with heartbeats (`ROBOT_ROTATION_ENABLED`).
- Ground Control: signed CloudEvents webhooks with an event catalog in `spec/events/v1` (`WEBHOOKS_ENABLED`).
- Ground Control: edge journal ingest. Heartbeats acknowledge journal uploads, and accepted records become log-only `Edge*` outbox events, once per record; `POST /satellites/sync` answers `413` above `SYNC_MAX_BODY_BYTES` (4 MiB).
- Ground Control: one `groundcontrol` executable for server and client.
- Ground Control: Prometheus `/metrics` on `METRICS_ADDR` (default `:9464`) with HTTP requests by route template, heartbeats and the heartbeat buffer, outbox pending and failed events, and the database pools.

### Fixed

- Satellite heartbeats were sent to a path Ground Control did not serve; credential refresh had no route.
- SPIFFE satellites failed heartbeat authentication.
- Registry secrets and response bodies were logged.
- A satellite in several groups could pull from the last synced group's projects only.
- A registration token could be used twice by concurrent requests.
- The cleanup job's advisory lock could leak through the connection pool.
- A re-tagged image was missing locally while its new digest replicated.
- A satellite that left its last group kept replicating it.
- A group could delete local content another group of the same satellite still wanted; a group removed from a satellite left its content behind; a tag dropped from a group stayed pullable while another tag kept its digest. Deletes now run after every group replicated, against the union of all groups' wanted artifacts, and pending deletes are kept in `state.json` (`retired`) until they succeed.
- Ground Control processes that started together raced on migrations, and all but one failed.
- `/login`, `/satellites/ztr`, `/satellites/spiffe-ztr` and `/oidc/*` read request bodies of any size; they now answer `413` above 64 KiB.
- Authenticated `/api` routes read request bodies of any size; they now answer `413` above `API_MAX_BODY_BYTES` (1 MiB), and `POST /api/groups/sync` above `GROUP_SYNC_MAX_BODY_BYTES` (16 MiB).
- A satellite session renewed during re-registration stayed valid for a full TTL.
- A journal batch that failed partway reused sequence numbers on the next flush.
- `groundcontrol worker` exited 0 when one job stopped early; it now fails.
- The satellite logged `invalid schedule provided for heartbeat_interval` (and the other two intervals) when the setting was simply absent. An absent interval now takes its default silently; an invalid one is still reported, with the rejected value.
- The OpenAPI spec did not list the `403` that `POST /satellites/ztr` and `GET /satellites/spiffe-ztr` answer for a revoked satellite, so generated clients had no type for it.
- `groundcontrol serve` kept answering `/health` healthy after a background job, such as the embedded async worker, stopped; `/health` now answers `503` with that job as an unhealthy component.
- Ground Control without `HARBOR_URL` failed with an error that did not name the variable.
- With `AUDIT_TRUST_FORWARDED_HEADERS=true`, the audit source IP, the per-IP failure limit and the anonymous limiter took the leftmost `X-Forwarded-For` entry, which the client writes. They now take the entry `AUDIT_TRUSTED_PROXY_HOPS` (default `1`) from the right, the one the trusted proxy appended.

### Known Limitations

- PostgreSQL 19 is the decided minimum (ADR-0020), but the version is not checked yet, and tests run on PostgreSQL 17.
- Keyless pulls through Harbor 3 workload identity federation are not available; `registry_auth.mode: workload_identity` is refused.
- Groups do not follow pushed tags automatically: Harbor has no registry event feed for Ground Control yet.
- The CLI has no commands for revocation, lifecycle, policies, role bindings, webhooks and the outbox; use the API.
- The proxy listens on loopback only and does not authenticate clients; peers need a registry endpoint such as a BYO registry.

## [0.0.6] - 2026-09-07

Last release before 1.0. See the [GitHub releases page](https://github.com/container-registry/harbor-satellite/releases) for 0.0.x notes.
