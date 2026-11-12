---
title: "Database Operations"
weight: 2
description: "Ground Control is the only component with a database. Satellites keep files only."
source: "docs/operations/database.md"
---

Ground Control is the only component with a database. Satellites keep files only.

## Supported PostgreSQL Versions

Ground Control needs PostgreSQL 19 or later ([ADR-0020](/docs/decisions/0020-postgresql-19-minimum/)). Before they migrate, `serve` and `worker` read `server_version_num` and `wal_level` and stop with an error when the server is older than 19.0 or runs `wal_level=minimal`. Against the `postgres:18.6` image the error reads:

```
PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) is not supported: Ground Control needs PostgreSQL 19 or later (ADR-0020); upgrade the server with pg_upgrade or dump and restore
```

Use PostgreSQL 19.0 or later in production. The compose files, the end-to-end and integration tests and the Helm chart's internal PostgreSQL run `postgres:19rc1`, which reports `server_version_num` 190000 and passes the check; release candidates are for tests only.

`wal_level` can stay at the default `replica`. This release does not create a logical replication slot, so the Ground Control role does not need the `REPLICATION` attribute yet; the logical outbox reader of ADR-0019 will need it.

The images of PostgreSQL 18 and later keep their data in `/var/lib/postgresql/<major>/docker`. Mount volumes at `/var/lib/postgresql`, not `/var/lib/postgresql/data`.

## Connection and TLS

`DB_SSLMODE` defaults to `require`. A server without TLS, such as the PostgreSQL in the bundled compose files, needs `DB_SSLMODE=disable`. For a server on another host use `verify-full` and point `DB_SSLROOTCERT` at its CA.

The Helm chart in `examples/deploy/helm/groundcontrol` sets `disable` for its internal PostgreSQL and `require` for an external one, unless `database.sslMode` says otherwise.

## Migrations

Migrations are embedded in the binary and run when `serve` or `worker` starts, with the API pool's URL (`DB_URL_API` or the `DB_*` settings). The container image also carries them in `/migrations`, which takes priority when it exists. The role needs the privileges to create and alter tables, indexes and partitions.

Replicas and workers that start at once take turns: each process holds a PostgreSQL advisory lock while it migrates, so only one applies migrations and the others wait, then find nothing left to apply.

## Pools per Plane

Ground Control keeps three pools:

| Plane | Serves | `application_name` |
|---|---|---|
| API | Operator routes, migrations, OIDC | `groundcontrol-api` |
| Fleet | Heartbeats, credential refresh, satellite authentication | `groundcontrol-fleet` |
| Async | Worker, cleanup | `groundcontrol-async` |

Fleet and async share the API pool unless they get their own URL or pool settings. See [Ground Control Configuration](/docs/reference/ground-control/#database).

Where the deployment can create roles, give each plane its own role, so the server enforces the connection budget and the timeouts even if Ground Control's settings are wrong:

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

Then set `DB_URL_API`, `DB_URL_FLEET` and `DB_URL_ASYNC` to URLs with those roles, and keep `DB_<PLANE>_MAX_OPEN_CONNS` below each `CONNECTION LIMIT`. The API role needs the DDL privileges for migrations. A managed service that allows one role only works with the single connection settings and the `DB_<PLANE>_*` pool settings.

## Data Retention

| Data | Retention | Job |
|---|---|---|
| `satellite_status` | Daily partitions, dropped after `STATUS_RETENTION_DAYS` (7) | Worker |
| Done outbox events | `OUTBOX_RETENTION` (168h), longer while an enabled webhook has not read them | Worker |
| Expired satellite sessions, OIDC login states | Deleted every `SESSION_EXPIRY_INTERVAL` | Worker |
| Expired registration tokens and operator sessions, old login attempts, orphaned artifacts | Deleted every 24 hours | `serve`, one replica at a time under an advisory lock |

## Inspecting the Outbox

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$GC/api/ops/outbox?state=failed"
```

Each entry names the event kind, the aggregate, the attempts and the last error. Events that failed because Harbor was unreachable are retried without limit and re-armed when Harbor is back. Any other failure stops after `OUTBOX_MAX_ATTEMPTS`; fix the cause, then change the aggregate again (for example sync the group) to queue a new event.
