---
status: proposed
date: 2026-10-15
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Ground Control async plane: transactional outbox and level-triggered publisher

## Context

Ground Control (GC) keeps the fleet in PostgreSQL, but every admin write also calls Harbor inside the request: robot accounts through the Harbor API and state artifacts through crane. Plan item 2 found these consequences at `508b0d6`:

* **Harbor work survives a rollback.** `SyncGroup` pushes the group state and updates every member's robot inside the database transaction (`server/group_handlers.go:67-112`, `server/helpers.go:36-97`). When a later statement fails, the transaction rolls back but the Harbor changes stay. `RegisterSatellite` deletes a robot it created on failure, but not the state artifacts it pushed (`server/satellite_handlers.go:131-197`).
* **Harbor down means GC down.** `serve` refuses to start without Harbor (`cli/root/serve.go:54-56`), and every admin write fails while Harbor is unreachable. The only way around is `SKIP_HARBOR_HEALTH_CHECK`, which makes ZTR hand out a hard-coded placeholder secret (`server/satellite_handlers.go:776-800`, `:825-831`).
* **Requests cost one Harbor round trip per member.** A `SyncGroup` of a group with 100 members takes 105 SQL statements, 201 Harbor API calls and 19 registry calls (`server/budget_test.go`).
* **State is pushed, not reconciled.** Nothing compares what GC wants published with what Harbor has. A push that failed after the commit stays missing until the next admin write touches the same object, and the last write wins when several touch it (D5).

## Decision Drivers

* An admin write is one database transaction: it commits completely or not at all, Harbor included.
* GC starts and accepts writes while Harbor is down, and converges once Harbor is back (gate G6).
* Requests cost a constant number of statements and no Harbor calls, whatever the size of the fleet.
* Several GC replicas and workers run against one database without coordination outside it.

## Decision

1. **Transactional outbox.** A request is one database transaction that changes the fleet and appends `fleet.Event`s through `fleet.Tx.Events()` (`event_outbox`, migration 017); it calls no registry, except ZTR (point 7). `Repository.Autocommit()` cannot append events. Every payload names its aggregate; delete events carry what the async plane needs once the rows are gone: every robot's Harbor ID (`robot_harbor_ids`, because a rotation can leave two), the SPIFFE ID and the state artifact reference. The payloads are the public event catalogue of plan item 9. `xid` records the appending transaction for ordered readers such as webhooks (plan item 9.1); the work queue does not need it. Kinds nobody handles, such as edge events, are log-only and inserted already done.
2. **Source tables and derived desired state.** The fleet tables stay the source of truth; a group's artifacts get their own table, `group_artifacts`, written only by the API transaction, so `groups` and its JSON stay as they are. `desired_state`, keyed by `(aggregate_type, aggregate_id)`, is purely derived: what GC last rendered (document, digest, `version`), what it published (`published_digest`) and the manifest digest the registry returned (`published_artifact_digest`), which satellites report back as the state they applied (plan item 4.3). Losing a row costs one push, never data. A satellite's rendered document covers its state artifact and its robot's projects (`fleet.RenderSatelliteDesiredState`), so a change of either is published.
3. **Level-triggered reconcilers.** The publisher claims due events and routes them by kind (`NewRoutes(handlers...)`, each handler names its kinds). A reconciler ignores what the event says: it renders the aggregate from the source tables, records the rendering (`RecordDesiredState`, which bumps `version` only when the digest changes) and publishes only while the rendered digest differs from the published one, so duplicated, reordered and replayed events converge and an unchanged aggregate costs no registry call. A reconciler whose aggregate is gone is done. There is no process-local cache of group state (#613, #619, #629 are not followed), so replicas cannot disagree.
   * The satellite reconciler grants the robot `fleet.RobotProjects` of the satellite's groups (supersedes D5's helper) and publishes the state, groupless included (D10). It also creates the robot when the satellite has none. New robots are named `fleet.RobotName` (`<satellite>-<id>`), unique per satellite ID: when the registry answers that the robot exists, an earlier attempt created it and crashed before storing it, and the robot of exactly that name is adopted, never a deleted predecessor's. Robots that exist keep their names; their rows already exist, so the reconciler never creates or adopts them.
   * The group reconciler publishes the group state and then appends one `SatelliteChanged` per member with a single `INSERT … SELECT` (`EventSink.AppendToGroupMembers`). Each member is then reconciled, locked and retried on its own, so a group of thousands neither outlives its lease nor replays the fan-out for one failing member.
   * The tombstone handler deletes the robots, the SPIRE entries, the state artifact and the desired-state row. State references and SPIFFE IDs are built from names, so when a live aggregate of the same name exists (deleted and registered again) it leaves the artifact and the entries alone and clears that aggregate's published digest, which republishes it.
4. **Claims with a lease.** `ClaimOutboxEvents` locks due rows with `FOR UPDATE SKIP LOCKED` in a materialized CTE, increments `attempts` and pushes `next_attempt_at` to the end of the lease. Workers pass the lease and every retry delay as durations and the database adds them to its own `NOW()`, so a worker's clock cannot shorten a lease or burn attempts. Workers never wait for each other, an event whose handler kills the process becomes due again after the lease, and `attempts` still counts that try. Handled events are completed; delivery is at least once, which the level-triggered handlers make harmless. An event without a handler is logged and completed. Other failures are retried with exponential backoff (`OUTBOX_BACKOFF_BASE` doubled per attempt up to `OUTBOX_BACKOFF_MAX`) until `OUTBOX_MAX_ATTEMPTS`, then stay visible at `GET /api/ops/outbox` (system admins).
5. **One worker per aggregate at a time.** Around each handler the publisher takes a session-level `pg_try_advisory_lock(namespace, id)` on a dedicated connection (`async.AggregateLocker`); the namespace per aggregate type comes from `internal/groundcontrol/advisorylock`, the one table of lock keys, which also holds the cleanup's ID. No transaction is open while the lock is held, so a registry call neither runs into `idle_in_transaction_session_timeout` nor holds back the snapshot xmin that ordered readers wait for. An event whose aggregate another worker holds is released (`OutboxStore.Release`), which gives its attempt back. Singleton jobs take a one-key ID from the same table with `TryAdvisoryXactLock`.
6. **Registry outages.** A failure that wraps `fleet.ErrRegistryUnavailable` is released, not failed, so an outage of any length costs no attempts. Every serve process runs `async.RegistryProbe` (with `--worker=off` too), which pings the registry every `HARBOR_PROBE_INTERVAL` without the rate limit. While the probe is stale (three intervals) the publisher claims nothing; when the registry is back it re-arms the events that ran out of attempts (`OutboxStore.Rearm`).
7. **ZTR stays synchronous, without the placeholder.** ZTR must return the robot secret: it claims the token on its own, rotates the secret outside any transaction, then stores the hash and consumes the token in one transaction; D7's claim makes a retry safe. Until the satellite reconciler has created the robot, ZTR answers 503 with `Retry-After` and gives the token back; the satellite's ZTR scheduler already retries. SPIFFE ZTR creates an unknown satellite the same way and answers 503 until its robot exists. The server tests drain the outbox after each registration (`drainOutbox`), so ZTR in tests keeps finding the robot. Plan item 8.4 removes this exception.
8. **Process model.** The publisher runs in `groundcontrol serve --worker=embedded` or alone as `groundcontrol worker`; any number of workers may run. A worker's jobs retry their own failures and return only when they cannot go on. `OUTBOX_CONCURRENCY` bounds handlers per worker and `HARBOR_RATE_LIMIT` the requests per second each worker sends to Harbor. The composition root (`internal/groundcontrol/app`) builds both from the database pools and one settings mapper and hands the server what it needs as options; the server does not import the async plane.
9. **Startup and health.** `serve` no longer checks Harbor at startup, and `/health` answers 200 with status `degraded` and a `components.harbor` entry while the probe is stale. `/health` is public, so the entry's `detail` is a fixed phrase, never a URL or error text. The database alone decides between healthy and unhealthy. `SKIP_HARBOR_HEALTH_CHECK` and its placeholder secret go away.
10. **PostgreSQL 14 or later.** `xid8` and `pg_current_xact_id()` need 13, which is end of life; the compose files use 16 and 17.

## Consequences

* Good: admin writes are atomic and keep working while Harbor is down; nothing dead-letters because of an outage.
* Good: request cost no longer grows with the fleet (`TestBudgetSyncGroup_ConstantInMembers`), and a large group fans out in one statement.
* Good: a lost or failed push is repaired by the next reconcile of the aggregate, and stuck events are visible.
* Bad: Harbor lags the database. Readers need the desired, published and applied digests to see where a satellite stands; plan item 4.3 exposes them.
* Bad: a satellite registered while Harbor is down waits for its robot (503 on ZTR).
* Bad: one dedicated connection per worker for the aggregate locks.
* Neutral: done rows accumulate; retention and partitioning by day come when the volume needs them, and keep every row above the lowest webhook checkpoint. Ordered consumers need the xid guard (ADR-0019).
* Open: GC keeps no SPIFFE ID for a satellite today; `fleet.Satellite.SPIFFEID` needs a home (for example a `satellite_identities` table) before the tombstone can name it.

## Implementation Status

| Part | Status |
|---|---|
| ADR | proposed |
| `event_outbox`, `group_artifacts`, `desired_state` (migration 017) and their queries, aggregate lock queries | done, SQL contract tests |
| `internal/groundcontrol/advisorylock` (lock keys) | done; the cleanup takes its ID from it |
| `async` publisher, router, reconcilers, tombstones, worker, registry probe and rate limit (2.3, 2.4) | skeleton, pending tests |
| `adapters/postgres` `OutboxStore`, `OutboxSink`, `DesiredStateStore`, `AggregateLocker` | skeleton, pending tests |
| fleet services as one transaction plus events (2.5) | skeleton, pending tests |
| `serve --worker` (default `off` until the worker works), hidden `groundcontrol worker` | flag validated; both modes are stubs |
| `GET /api/ops/outbox`, ZTR 503, `/health` `degraded` in the spec (2.5, 2.6) | spec and generated code; handlers unchanged or 501 |
| `OUTBOX_*`, `HARBOR_RATE_LIMIT`, `HARBOR_PROBE_INTERVAL` | parsed, not used |

## Validation

* The pending tests of plan item 2 pass without their markers: `internal/groundcontrol/fleet` (the 2.5 service tests), `internal/groundcontrol/async`, `internal/groundcontrol/adapters/postgres` (`TestPublisher_KillMidBatchConverges`, `TestAggregateLocker_ExcludesOtherWorkers` among them), `internal/groundcontrol/server` (`TestHarborDown_AdminWritesSucceedAndConverge`, `TestBudgetSyncGroup_ConstantInMembers`, `TestZtr_Returns503UntilRobotProvisioned`, `TestZtr_NeverReturnsPlaceholderSecret`, `TestHealth_ReportsHarborDegraded`) and `internal/groundcontrol/cli/root` (`TestServe_StartsWithHarborUnreachable`).
* The golden API responses stay unchanged, except where 2.5 and 2.6 change them on purpose.
