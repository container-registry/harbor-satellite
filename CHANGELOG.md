# Changelog

All notable changes to Harbor Satellite are recorded here. Versions follow [Semantic Versioning](https://semver.org/).

## [1.0.0] - Unreleased

1.0 is the first release with a stable configuration, API and on-disk format. Upgrade notes and every breaking change are in [Upgrading to 1.0](docs/operations/upgrading.md#upgrading-to-10-from-00x). The Ground Control API reports version `1.0.0` (`spec/groundcontrol/openapi.yaml`), and the Helm chart is 0.3.0 with `appVersion` `1.0.0`.

### Upgrade Notes

1. Upgrade PostgreSQL to 19 before Ground Control, with `pg_upgrade` or dump and restore. Ground Control 1.0 refuses older servers, and no release supports both ranges. Production must run PostgreSQL 19.0 or later; the `postgres:19rc1` image in the compose files and the Helm chart is for tests and evaluation only.
2. With the Helm chart's internal PostgreSQL or the repository's `docker-compose.yml`, dump the database before the upgrade and restore it afterwards, see [PostgreSQL 19](docs/operations/upgrading.md#postgresql-19). The new volume layout does not pick up the old data directory.
3. Set `DB_SSLMODE=disable` for a PostgreSQL without TLS, and check `robot_accounts` for duplicates before migration 023 runs.
4. Back up the database, stop every Ground Control process, start one of the new version, then the rest, then upgrade the satellites. The full procedure is in [Upgrading](docs/operations/upgrading.md).

### Breaking Changes

- Ground Control: PostgreSQL 19 is the minimum (ADR-0020). `serve` and `worker` read `server_version_num` and `wal_level` before they migrate and stop when the server is older than 19.0 (the error names the version found) or runs `wal_level=minimal`. Release candidates of 19 report 190000 and pass.
- Helm chart and `docker-compose.yml`: the PostgreSQL volume is mounted at `/var/lib/postgresql` instead of `/var/lib/postgresql/data`, because images of 18 and later keep their data in `/var/lib/postgresql/<major>/docker`. The chart no longer sets `PGDATA` (was `/var/lib/postgresql/data/pgdata`). A volume from an earlier chart or compose file is not migrated: the new server initializes an empty cluster in `/var/lib/postgresql/19/docker` and the old data stays unused next to it.
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
- Ground Control: a new satellite may not be named `active` or `stale`, and a new group may not be named `satellite`; registration (token, SPIFFE and SPIFFE ZTR) and `POST /api/groups/sync` answer `400`. The router matches `GET /api/satellites/active` and `/stale` before `GET /api/satellites/{satellite}`, and `DELETE /api/groups/satellite` before `DELETE /api/groups/{group}`, so such a satellite could not be fetched and such a group not deleted. Existing satellites and groups with these names keep working, apart from those two routes.

### Added

- Satellite: pull-only OCI proxy on `127.0.0.1:8585` in `proxy` and `replica` modes, used as the container runtime mirror endpoint.
- Satellite: streaming pull-through (`PROXY_STREAMING`), with background graph completion.
- Satellite: proxy write deadline and connection cap (`PROXY_WRITE_TIMEOUT`, `PROXY_MAX_CONNECTIONS`).
- Satellite: `/healthz` and Prometheus `/metrics` for pulls, pull-through fills, replication, index lock waits, heartbeats (`satellite_heartbeats_total`) and journal acknowledgements (`satellite_journal_acks_total`, `satellite_journal_acked_seq`).
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
- Ground Control CLI: `revoke`/`unrevoke satellite`, `get satellite-lifecycle`, policies, role bindings, webhooks (secret from `GROUND_CONTROL_WEBHOOK_SECRET`) and `get outbox-events`.
- Helm chart: `extraEnv` and `extraEnvFrom` to set any Ground Control variable, secrets included.
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
- The containerd `hosts.toml` of a registry given with a scheme (`--mirrors=containerd:https://quay.io` or a `registry_fallback` entry) was written to `certs.d/https:/quay.io`, which containerd never reads; it now goes to `certs.d/<host[:port]>`, and a name that is not a host is refused.
- Rewriting `hosts.toml` dropped every key the satellite does not manage, such as `ca`, `skip_verify` and `header`; rewriting the CRI-O `registries.conf` dropped `prefix`, `blocked`, `mirror-by-digest-only` and `pull-from-mirror`. Both now keep them.
- The satellite stopped noticing changes to `config.json` once a tool replaced the file by renaming over it, and lost a change written while a reload was running.
- A `heartbeat_interval` from Ground Control took effect only after a restart, while every heartbeat already reported the new interval. A satellite that started with the 30s default and got a 10s config beat every 30s, so its stale threshold in Ground Control (three reported intervals, 30s) was no longer than the gap between beats. The heartbeat interval is now applied on hot reload.
- `@every 0s` or a negative interval passed validation and crashed the satellite at startup or on hot reload; it is now rejected and the default is used.
- Ground Control said `ROBOT_ROTATION_ENABLED needs DATA_ENCRYPTION_KEY` when `WEBHOOKS_ENABLED` asked for the key; the message now names the setting that needs it.
- A SPIRE entry create that SPIRE refused was reported as an empty response, and a refused entry delete as success, which left a deleted satellite's entry in SPIRE.
- Ground Control without `HARBOR_URL` failed with an error that did not name the variable.
- With `AUDIT_TRUST_FORWARDED_HEADERS=true`, the audit source IP, the per-IP failure limit and the anonymous limiter took the leftmost `X-Forwarded-For` entry, which the client writes. They now take the entry `AUDIT_TRUSTED_PROXY_HOPS` (default `1`) from the right, the one the trusted proxy appended.

### Known Limitations

- Webhook delivery polls the outbox and waits for the oldest open transaction on the server. The logical decoding reader of ADR-0019 is not implemented, so the Ground Control role needs no `REPLICATION` attribute yet. There is no per-subscription delivery-lag metric.
- Keyless pulls through Harbor 3 workload identity federation are not available; `registry_auth.mode: workload_identity` is refused.
- Groups do not follow pushed tags automatically: Harbor has no registry event feed for Ground Control yet.
- The proxy listens on loopback only and does not authenticate clients; peers need a registry endpoint such as a BYO registry.

## [0.0.6] - 2026-09-07

Last release before 1.0. See the [GitHub releases page](https://github.com/container-registry/harbor-satellite/releases) for 0.0.x notes.
