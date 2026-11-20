---
status: accepted
date: 2026-10-16
updated: 2026-11-20
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# PostgreSQL 19 as the minimum version

## Context

Ground Control is the only Harbor Satellite component that uses PostgreSQL; satellites keep no database. ADR-0014 (decision 10) set the minimum to 14, because `xid8` and `pg_current_xact_id()` need 13. The compose files and the Helm chart use 15, 16 and 17.

With 14 as the floor, the outbox of ADR-0014 and ADR-0019 is a polled table:

* **Webhook delivery waits for the cluster-wide xmin.** `ReadOutboxAfter` returns only rows below `pg_snapshot_xmin(pg_current_snapshot())`, so any open transaction on the server holds every subscription back.
* **Every event costs churn.** The publisher and the dispatcher poll every second, and an event is inserted, updated per claim, updated on completion and deleted by retention.
* **Logical decoding is not safe to depend on before 17.** Releases before 17 lose logical slots on failover and on `pg_upgrade`, and before 18 a dead consumer's slot pins WAL until the disk fills unless `max_slot_wal_keep_size` is set.

The release notes of 17, 18 and 19 remove these limits:

* 17: failover slots (`sync_replication_slots`, `synchronized_standby_slots`), logical slots kept across `pg_upgrade` from 17 on, `transaction_timeout`.
* 18: `idle_replication_slot_timeout`, `autovacuum_vacuum_max_threshold`, eager freezing.
* 19: with `wal_level=replica` (the default), creating the first logical slot raises `effective_wal_level` to `logical` without a restart, and dropping or invalidating the last one lowers it again. NOTIFY wakes only the backends that listen. `REPACK (CONCURRENTLY)` rebuilds a table without blocking writes. `INSERT ... ON CONFLICT DO SELECT ... RETURNING` returns the existing row without writing it.

PostgreSQL 19.0 was released on 2026-11-19, and Docker Hub has published `postgres:19` and `postgres:19-alpine` since 2026-11-20. The final release notes keep all three behaviours this record was accepted on at RC1. Checked on `postgres:19` (19.0, 2026-11-20):

* Creating a `pgoutput` slot at `wal_level=replica` raises `effective_wal_level` to `logical`. Dropping it lowers the level to `replica` a few seconds later, asynchronously, with the log line "logical decoding is disabled because there are no valid logical replication slots".
* Creating a slot without the `REPLICATION` attribute fails with "permission denied to use replication slots".
* `REPACK (CONCURRENTLY)` works for a table owner without `REPLICATION` or superuser. For the length of the repack it turns on logical decoding, and it takes one of `max_repack_replication_slots` (default 5).
* `ON CONFLICT (reference) DO SELECT RETURNING id` in a CTE returns both new and existing rows, and `n_tup_upd` stays 0.
* Defaults: `idle_replication_slot_timeout = 0` and `max_slot_wal_keep_size = -1`, so neither bound is on. `max_locks_per_transaction = 128`, `jit = off`, `log_lock_waits = on`, `default_toast_compression = lz4`.

Of the 22 incompatibilities in "Migration to Version 19", none touches Ground Control's schema or queries: no `inet`/`cidr` `btree_gist` indexes (which `pg_upgrade` refuses), no full-text search, `pg_trgm` or `fuzzystrmatch`, no `E''` strings, and no CR or LF in names. Two matter to operators: a warning after every MD5 password login (`md5_password_warnings`), and RADIUS authentication is removed.

## Decision Drivers

* Ground Control can depend on logical decoding without operators changing `wal_level` or restarting the server.
* A failover, a major upgrade or a dead consumer neither loses events nor fills the disk.
* One supported version range, so the design does not carry fallbacks for releases it is moving away from.

## Decision

1. **The minimum is PostgreSQL 19.0.** Supersedes ADR-0014 decision 10. Ground Control refuses to start when `server_version_num` is below 190000 and names the version it found.
2. **Logical decoding is part of the design (ADR-0019, decision 6).** The webhook dispatcher reads `event_outbox` inserts from one logical slot, and Ground Control's database role needs the `REPLICATION` attribute to create it. A server with `wal_level=minimal` is refused at startup, because no slot can be created there.
3. **Test and example images move to `postgres:19` now.** The tag exists since 2026-11-20. The integration, E2E, compose and example stacks, the Helm chart and the quickstart pin `postgres:19` (`19-alpine` where the stack used alpine). No stack runs a beta or a release candidate.
4. **Existing installs upgrade PostgreSQL before Ground Control.** The release notes of the first release with this floor say so and point to `pg_upgrade` or dump and restore with the 19 `pg_dump`. No Ground Control release supports both ranges.
5. **Slot bounds are set, not left to the defaults.** Both are off on 19.0, so the Helm chart and the deployment docs set `max_slot_wal_keep_size` and `idle_replication_slot_timeout`, and Ground Control logs a warning at startup when either is off. Ground Control's role stores its password as SCRAM-SHA-256.
6. **Get-or-create statements use `ON CONFLICT DO SELECT`.** The no-op update `ON CONFLICT (reference) DO UPDATE SET reference = EXCLUDED.reference` in `satellite_view.sql`, which exists only to make `RETURNING` see existing rows, becomes `DO SELECT`. `BatchInsertArtifacts` (`DO NOTHING`) and `GetArtifactIDsByReferences` become one statement. Upserts that do change columns keep `DO UPDATE`.

## Consequences

* Good: the outbox can drop the xmin guard and the polling (ADR-0019, decision 6), and slots survive failover and major upgrades.
* Good: operators do not set `wal_level`; a logical slot raises it while it exists.
* Good: get-or-create writes no dead tuple per existing artifact on every inventory flush.
* Bad: installs on 14 to 18 have to upgrade PostgreSQL before they can upgrade Ground Control.
* Bad: a managed PostgreSQL service can host Ground Control only once it offers 19 and allows logical slots for the Ground Control role.
* Neutral: the extra WAL written while a logical slot exists is the cost of logical decoding; `effective_wal_level` shows when it applies. `REPACK (CONCURRENTLY)` adds the same cost for the length of a repack.
* Neutral: the 19 behaviours this record relies on were checked on 19.0; a later minor release that changes them reopens it.

## Implementation Status

| Part | Status |
|---|---|
| ADR | accepted |
| Startup check of `server_version_num`, `wal_level` and the slot bounds | not started |
| `postgres:19` in `test/integration`, `test/e2e/docker`, `docker-compose.yml`, `examples/deploy`, the Helm chart and the quickstart | not started; image available since 2026-11-20 |
| `REPLICATION` attribute, SCRAM and slot bounds in the deployment docs and the Helm chart | not started |
| `ON CONFLICT DO SELECT` in `satellite_view.sql` and the artifact batch insert | not started |
| Release notes of the first release with the floor | not started |

## Validation

* `task test:integration` and `task e2e` pass on `postgres:19`.
* Ground Control started against PostgreSQL 18 exits with an error that names the version.
* After a failover to a standby with `sync_replication_slots` on, the dispatcher resumes from its slot without a gap or a duplicate beyond at least once.
* After the dispatcher's slot is dropped, `effective_wal_level` returns to `replica`.
* A SQL contract test shows the artifact get-or-create returning existing and new ids with no update to existing rows.
