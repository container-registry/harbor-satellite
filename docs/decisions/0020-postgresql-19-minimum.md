---
status: accepted
date: 2026-10-16
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
* 19: with `wal_level=replica` (the default), creating the first logical slot raises `effective_wal_level` to `logical` without a restart, and dropping or invalidating the last one lowers it again. NOTIFY wakes only the backends that listen. `REPACK (CONCURRENTLY)` rebuilds a table without blocking writes.

On 2026-10-16, 19 is at RC1 (released 2026-10-15), and Docker Hub has `postgres:19beta3` but no `19`, `19rc1` or `19-alpine` tag.

## Decision Drivers

* Ground Control can depend on logical decoding without operators changing `wal_level` or restarting the server.
* A failover, a major upgrade or a dead consumer neither loses events nor fills the disk.
* One supported version range, so the design does not carry fallbacks for releases it is moving away from.

## Decision

1. **The minimum is PostgreSQL 19.0.** Supersedes ADR-0014 decision 10. Ground Control refuses to start when `server_version_num` is below 190000 and names the version it found.
2. **Logical decoding is part of the design (ADR-0019, decision 6).** The webhook dispatcher reads `event_outbox` inserts from one logical slot, and Ground Control's database role needs the `REPLICATION` attribute to create it. A server with `wal_level=minimal` is refused at startup, because no slot can be created there.
3. **Test and example images move to `postgres:19` at the 19.0 release.** Until Docker Hub has that tag, the integration, E2E and compose stacks stay on their current images, and no stack runs a beta or a release candidate.
4. **Existing installs upgrade PostgreSQL before Ground Control.** The release notes of the first release with this floor say so and point to `pg_upgrade` or dump and restore. No Ground Control release supports both ranges.

## Consequences

* Good: the outbox can drop the xmin guard and the polling (ADR-0019, decision 6), and slots survive failover and major upgrades.
* Good: operators do not set `wal_level`; a logical slot raises it while it exists.
* Bad: installs on 14 to 18 have to upgrade PostgreSQL before they can upgrade Ground Control.
* Bad: a managed PostgreSQL service can host Ground Control only once it offers 19 and allows logical slots for the Ground Control role.
* Bad: the design depends on a release that is not final; a change between RC1 and 19.0 to automatic logical decoding, NOTIFY or REPACK reopens this record.
* Neutral: the extra WAL written while a logical slot exists is the cost of logical decoding; `effective_wal_level` shows when it applies.

## Implementation Status

| Part | Status |
|---|---|
| ADR | accepted |
| Startup check of `server_version_num` and `wal_level` | not started |
| `postgres:19` in `test/integration`, `test/e2e/docker`, `docker-compose.yml`, `examples/deploy`, the Helm chart and the quickstart | waits for the 19.0 image |
| `REPLICATION` attribute in the deployment docs and the Helm chart | not started |
| Release notes of the first release with the floor | not started |

## Validation

* `task test:integration` and `task e2e` pass on `postgres:19`.
* Ground Control started against PostgreSQL 18 exits with an error that names the version.
* After a failover to a standby with `sync_replication_slots` on, the dispatcher resumes from its slot without a gap or a duplicate beyond at least once.
