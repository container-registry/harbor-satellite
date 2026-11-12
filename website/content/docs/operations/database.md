---
title: "Database Operations"
weight: 2
description: "Ground Control is the only component with a database. Satellites keep files only."
source: "docs/operations/database.md"
---

Ground Control is the only component with a database. Satellites keep files only.

## Supported PostgreSQL Versions

[ADR-0020](/docs/decisions/0020-postgresql-19-minimum/) sets PostgreSQL 19 as the minimum for Ground Control, because the planned outbox reader depends on logical decoding features of 17 to 19. This release does not check the server version at startup yet, and its CI, integration and end-to-end tests run on PostgreSQL 17. The schema of this release uses `xid8`, `pg_current_xact_id()` and declarative partitioning, so it needs at least PostgreSQL 13.

Plan new installations on PostgreSQL 19 once your platform offers it. Existing installations on 13 to 18 keep working with this release; a later release will refuse to start below 19, so plan the PostgreSQL upgrade before that one.

## Connection and TLS

`DB_SSLMODE` defaults to `require`. A server without TLS, such as the PostgreSQL in the bundled compose files, needs `DB_SSLMODE=disable`. For a server on another host use `verify-full` and point `DB_SSLROOTCERT` at its CA.

The Helm chart in `examples/deploy/helm/groundcontrol` sets `disable` for its internal PostgreSQL and `require` for an external one, unless `database.sslMode` says otherwise.

## Migrations

Migrations are embedded in the binary and run when `serve` or `worker` starts, with the API pool's URL (`DB_URL_API` or the `DB_*` settings). The container image also carries them in `/migrations`, which takes priority when it exists. The role needs the privileges to create and alter tables, indexes and partitions.

When several replicas start at once, start one first and let it finish the migrations, then the others.

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
