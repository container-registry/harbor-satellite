---
title: "Upgrading"
weight: 1
description: "Ground Control migrations are forward only in production. To go back to an older version, restore the backup taken in step 2."
source: "docs/operations/upgrading.md"
---

## General Procedure

1. Read the release notes in [CHANGELOG.md](/docs/changelog/) for breaking changes.
2. Back up the Ground Control database, see [Backup and Restore](/docs/operations/backup-and-restore/).
3. Stop every Ground Control process, `serve` and `worker`.
4. Start one Ground Control process of the new version and wait until `/health` answers. It applies the migrations at startup.
5. Start the remaining Ground Control processes.
6. Upgrade the satellites. Ground Control accepts heartbeats from older satellites, so they can follow site by site.

Ground Control migrations are forward only in production. To go back to an older version, restore the backup taken in step 2.

## Upgrading to 1.0 from 0.0.x

### Before You Start

- **PostgreSQL 19.** Ground Control 1.0 refuses a server older than 19.0. Upgrade PostgreSQL first, see [PostgreSQL 19](#postgresql-19).
- **Database TLS.** `DB_SSLMODE` now defaults to `require`; earlier versions always connected with `sslmode=disable`. A PostgreSQL without TLS needs `DB_SSLMODE=disable`, or Ground Control stops at startup. The compose files in this repository and the Helm chart's internal PostgreSQL set `disable`. For an external database the chart now uses `require` unless `database.sslMode` is set.
- **Duplicate robot rows.** Migration 023 stops when a satellite has more than one row in `robot_accounts`. Check before upgrading, see [below](#migration-023-constraints-and-time-zones).
- **Time zone.** Migration 023 converts every timestamp column to `TIMESTAMPTZ`, reading stored values as UTC. Check that the database `TimeZone` and the Ground Control process both ran in UTC.
- **Status table size.** Migration 024 copies `satellite_status` into a partitioned table in one transaction. On a large table this takes time and disk space; delete old rows first if you do not need them.
- **Worker.** Changes to groups, configs and satellites now reach Harbor through the async worker. `serve` runs it by default. If you run `serve --worker=off`, also run `groundcontrol worker`.
- **Rate limits.** `RATE_LIMIT_SPLIT` now defaults to `true`: satellites are limited per satellite (`RATE_LIMIT_FLEET`, 3/1s), ZTR and OIDC per client IP (`RATE_LIMIT_ANONYMOUS`, 10/1m), and `/login` only by the failure limit and lockout. Behind a reverse proxy also set `AUDIT_TRUST_FORWARDED_HEADERS=true`, or every ZTR shares the proxy address. `RATE_LIMIT_SPLIT=false` restores the shared limiter of 10 requests per minute per IP.

### Breaking Changes

| Area | Change | Action |
|---|---|---|
| Ground Control | PostgreSQL 19 minimum; `serve` and `worker` stop below 19.0 or with `wal_level=minimal`. | Upgrade PostgreSQL first, see [PostgreSQL 19](#postgresql-19). |
| Helm chart, `docker-compose.yml` | The PostgreSQL volume is mounted at `/var/lib/postgresql`, and the chart no longer sets `PGDATA`. An old volume's data is not picked up. | Dump before, restore after, see [PostgreSQL 19](#postgresql-19). |
| Ground Control | `DB_SSLMODE` default `require`. | Set `DB_SSLMODE=disable` for a server without TLS. |
| Ground Control | Starts while Harbor is unreachable and reports it in `/health` as `degraded`. Admin writes are queued and applied by the worker. | Use `/health` status and `GET /api/ops/outbox` in monitoring instead of startup failures. |
| Ground Control | Token registration answers `503` with `Retry-After` until the worker created the satellite's robot. A used token answers `400`, an expired one `401`. | Satellites retry by themselves. Scripts that call ZTR must retry on `503`. |
| Ground Control | Concurrent `PATCH /api/configs/{config}` answers `409` instead of overwriting. Deleting a config that satellites use is refused by the database too. | Retry the patch after reading the config again. |
| Ground Control | Config documents are validated before they are stored: `422` on invalid documents. | Fix the key named in the message. |
| Ground Control | Basic auth on `/api` counts toward the account lockout. More than `AUTH_FAILURE_LIMIT` (20) failures per IP per minute answer `429`. | Fix clients with wrong credentials. |
| Ground Control | Request bodies of `/login`, ZTR and the OIDC routes are limited to 64 KiB, `POST /api/groups/sync` to 16 MiB (`GROUP_SYNC_MAX_BODY_BYTES`) and other `/api` routes to 1 MiB (`API_MAX_BODY_BYTES`). | Raise the limit for a group of more than about 10000 artifacts. |
| Ground Control | `RATE_LIMIT_SPLIT` defaults to `true`: sync and refresh 3/1s per satellite, ZTR and OIDC 10/1m per client IP, `/login` only by the failure limit. | Behind a proxy set `AUDIT_TRUST_FORWARDED_HEADERS=true`; set `RATE_LIMIT_SPLIT=false` for the old shared limiter. |
| Ground Control | `SKIP_HARBOR_HEALTH_CHECK` only silences the startup log line. SPIFFE registration no longer hands out placeholder credentials. | Remove it from production settings. |
| Ground Control | Satellite status rows are kept in daily partitions and dropped after `STATUS_RETENTION_DAYS` (7). | Export status history you need before upgrading. |
| Satellite | An `http://` Ground Control URL is refused unless `use_unsecure` is set. | Use HTTPS, or set `USE_UNSECURE=true` in development. |
| Satellite | Heartbeats go to `POST /satellites/sync` and credential refresh to `POST /satellites/refresh`. 0.0.x satellites sent heartbeats to a path Ground Control never served. | Upgrade satellites to get fleet status. |
| Satellite | `/healthz` and `/metrics` listen on `127.0.0.1:8586`. A port in use stops startup. | Run one satellite per host, or set `TELEMETRY_ADDR`. |
| Satellite | Unreferenced content in the OCI layout is deleted after `STORE_GC_GRACE` (10 minutes) instead of at once. | Plan disk space for content removed in the last 10 minutes, or set `STORE_GC_GRACE=0`. |
| Satellite | A group change replicates first and deletes afterwards, and a satellite that leaves its last group stops replicating it. | None. |
| Satellite | `encrypt_config` needs the device-bound encryptor of the release binary. A sealed `config.json` is refused by builds without it. | Use release builds, or the default build tags. |

### Migration 023: Constraints and Time Zones

Migration 023 adds `UNIQUE (satellite_id)` to `robot_accounts`, makes deleting a config that satellites use fail (`ON DELETE RESTRICT`), checks `satellites.heartbeat_interval` against `@every HHhMMmSSs`, adds `configs.version`, and turns every `TIMESTAMP` column into `TIMESTAMPTZ`.

**Duplicate robot accounts.** The migration stops with `robot_accounts has several robots for satellites: ...` when a satellite has more than one row in `robot_accounts`. List them:

```sql
SELECT satellite_id, id, robot_name, robot_id, created_at
FROM robot_accounts
WHERE satellite_id IN (SELECT satellite_id FROM robot_accounts GROUP BY satellite_id HAVING count(*) > 1)
ORDER BY satellite_id, created_at DESC, id DESC;
```

Keep the newest row per satellite and delete the others:

```sql
DELETE FROM robot_accounts r
USING robot_accounts newer
WHERE newer.satellite_id = r.satellite_id
  AND (newer.created_at, newer.id) > (r.created_at, r.id);
```

Delete the Harbor robots of the removed rows (`robot_id`) in Harbor as well, then start Ground Control again.

**Time zones.** The conversion reads every stored value as UTC wall-clock time (`AT TIME ZONE 'UTC'`). Values written with `NOW()` follow the database's `TimeZone` setting, and values written by Ground Control (for example `robot_expiry`, `expires_at`, `locked_until`) follow the time zone of the Ground Control process. If either was not UTC, adjust the stored values before upgrading.

### After the Upgrade

- Satellites that registered with 0.0.x keep authenticating with their robot credentials (`SATELLITE_ROBOT_AUTH=true`) and receive a satellite session with their next heartbeat.
- Check `GET /api/ops/outbox?state=failed` is empty, and `GET /api/satellites/{satellite}/lifecycle` shows `converged` for upgraded satellites.
- Consider `ROBOT_ROTATION_ENABLED=true` with a `DATA_ENCRYPTION_KEY`, so robots are replaced before `ROBOT_DURATION_DAYS` runs out.

### PostgreSQL 19

Upgrade PostgreSQL to 19 while the old Ground Control is stopped, then start Ground Control 1.0. Use PostgreSQL 19.0 or later in production; `postgres:19rc1`, which the compose files and the chart use, is for tests and evaluation. See [Database Operations](/docs/operations/database/#supported-postgresql-versions).

**External server.** Use `pg_upgrade` or dump and restore, following the PostgreSQL or your provider's documentation. Keep `wal_level` at `replica` or `logical`.

**Helm chart internal PostgreSQL or `docker-compose.yml`.** Earlier charts ran `postgres:16` with `PGDATA=/var/lib/postgresql/data/pgdata` on a volume mounted at `/var/lib/postgresql/data`, and the compose file mounted its `pgdata` volume there too. The new layout mounts the volume at `/var/lib/postgresql` and the server keeps its data in `/var/lib/postgresql/19/docker`. The old cluster is neither upgraded nor read: PostgreSQL 19 initializes an empty one, and Ground Control then starts on an empty database. Move the data with dump and restore:

1. With the old release still running, dump the database: `kubectl exec <release>-ground-control-postgresql-0 -- pg_dump -U <user> -Fc <database> > gc.dump` (compose: `docker compose exec -T postgres pg_dump -U postgres -Fc groundcontrol > gc.dump`).
2. Scale Ground Control to zero, upgrade the chart (or the compose file), and wait until the new PostgreSQL is ready. Keep Ground Control stopped.
3. Restore: `kubectl exec -i <release>-ground-control-postgresql-0 -- pg_restore -U <user> -d <database> --clean --if-exists < gc.dump`.
4. Start Ground Control. It runs migrations 016 to 024 on a database restored from 0.0.6.

The old files stay on the volume (`/var/lib/postgresql/pgdata` for the chart) until you delete them.

## PostgreSQL Major Upgrades

Upgrade PostgreSQL with `pg_upgrade` or dump and restore while Ground Control is stopped, then start Ground Control. Ground Control 1.0 supports PostgreSQL 19 and later, see [Database Operations](/docs/operations/database/#supported-postgresql-versions).
