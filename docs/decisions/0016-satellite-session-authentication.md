---
status: proposed
date: 2026-10-16
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Satellite sessions and the fleet hot path

## Context

Plan item 4 (fleet hot path) found these limits at `508b0d6`:

* **Every heartbeat is expensive.** `POST /satellites/sync` verifies the robot secret with Argon2id and looks the robot and the satellite up on every beat; `TestBudgetHeartbeat` measures 8 statements per beat.
* **Ground Control authentication is tied to the robot.** The satellite authenticates to Ground Control with the Harbor robot it pulls with, so the robot's lifetime and rotation decide whether a satellite can report.
* **Lists are computed per request.** The active and stale lists run LATERAL queries over `satellite_status`; `STALE_THRESHOLD` is parsed and never read.
* **One limiter for everything.** A single per-IP limiter (10 per minute) guards login, ZTR and every satellite beat, so a fleet behind one NAT is throttled.
* **ADR-0006 (lifecycle states) is not implemented.** A satellite cannot be revoked, and Ground Control keeps no SPIFFE ID for a satellite (ADR-0014's open item).

## Decision Drivers

* A plain heartbeat costs at most one statement on average and no Argon2id.
* Revoking a satellite or deleting it takes effect on every replica within a bounded time.
* Existing satellites keep working without a new onboarding.
* The golden JSON of `satellites` and `satellite_status` stays as it is.

## Decision

1. **Sessions (G3, 4.1).**
   * ZTR issues a Ground Control session: an `hss_` token (`"hss_" + base64url(32 random bytes)`), stored as its SHA-256 in `satellite_sessions` (migration 019) and looked up by hash.
   * A positive cache per replica (`SATELLITE_SESSION_CACHE_TTL`, at most 5 minutes) bounds how long a deleted session or a revocation takes to reach another replica.
   * One authentication step: an SVID is authoritative, then a Bearer session, then robot Basic while `SATELLITE_ROBOT_AUTH` is on. Failures count in D6's per-IP `FailureLimiter`, checked before the Argon2id verify.
   * A session is renewed in the last third of its TTL; a predecessor has at most one successor.
   * Migration: a robot-Basic beat of a satellite without a session gets one, once.
   * SPIFFE IDs live in `satellite_identities`, so the tombstone can name them.
2. **Hot path (4.2, 4.4).**
   * Each replica buffers heartbeats (`internal/groundcontrol/heartbeat`) and flushes them every `HEARTBEAT_FLUSH_INTERVAL` in one transaction. A flush never moves a value backwards, and a poison row affects only its own satellite, which is dropped after `HEARTBEAT_MAX_FLUSH_FAILURES`.
   * Inventory digest (`internal/shared/inventory`): the satellite sends `cached_images` only when the digest changed or Ground Control asked with `inventory_requested`.
   * Journal ingestion (ADR-0015) goes through `HeartbeatService.Beat`, which records the beat before ingesting; a malformed journal answers 200 with an ack error.
   * Statements: a plain beat at most 1 on average, a journaled beat at most 4, at most 5 with a session renewal. ZTR may rise from 9 to 11; it is a once-per-satellite cold path.
3. **Lifecycle (4.3, ADR-0006).**
   * `satellite_view` is the read model of the four states (`pending`, `online`, `stale`, `revoked`), because golden JSON forbids new `satellites` columns. It becomes the only owner of `last_seen` once the readers move to it.
   * Revoke answers 403 on sync and both ZTR flavours and keeps the sessions; the satellite reconciler disables the robot with its projects kept (`SetRobotAccess`, because Harbor rejects an empty permission list). Unrevoke returns the satellite to pending.
   * The stale detector runs in the worker under a singleton lock, with a floor of twice the flush interval, and appends a log-only `SatelliteStale`.
   * Convergence is per group state reference.
4. **Limits (4.6).** With `RATE_LIMIT_SPLIT`: anonymous endpoints per client IP (`RATE_LIMIT_ANONYMOUS`), satellites per satellite (`RATE_LIMIT_FLEET`, `"3/1s"`), login through the `FailureLimiter` only; 429 is an `AppError` body. The anonymous key is the client IP as `clientIP` reads it: with `TRUST_FORWARDED_HEADERS` the leftmost `X-Forwarded-For` entry, so the limit is only as strong as the proxy's header hygiene.
5. **Partitions (4.5).** `satellite_status` partitioning is pending SQL plus a maintainer that owns retention once partitions land.

## Consequences

* Good: a beat costs no Argon2id and at most one statement on average; a fleet behind one NAT is not throttled; revocation exists.
* Bad: state lags by up to the flush interval, and a replica crash loses at most one interval of beats.
* Bad: revocation and session deletion take up to the cache TTL on other replicas.
* Neutral: supersedes the satellite part of ADR-0004.

## Implementation Status

| Part | Status |
|---|---|
| ADR | proposed |
| Migration `019_satellite_sessions_and_view.sql` and its queries | done, SQL contract tests |
| `authn`, `heartbeat`, `fleet.HeartbeatService`, lifecycle services, stale detector, session expirer, postgres adapters | skeleton, pending tests |
| Sync and ZTR contract (`session`, `inventory_requested`, `inventory_digest`), lifecycle routes | in the spec; the routes answer 501 |
| Split limiters (`RATE_LIMIT_SPLIT`) | `ParseRate` done; `Limiter` and `LimitBy` stubs answer 501, so the flag stays off until 4.6 |
| `satellite_status` partitions | pending SQL, maintainer stub |

## Validation

* The pending tests of plan item 4 pass without their markers, among them `TestBudgetHeartbeat_AtMostOneStatementPerBeat`, `TestBudgetHeartbeat_JournaledBeatCostsAtMostFour` and `TestBudgetHeartbeat_JournaledRenewingBeatCostsAtMostFive`.
* `task test:load` (`TestFleetLoad5000`) sees no 429 and a flat p99.
