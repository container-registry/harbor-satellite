---
status: proposed
date: 2026-10-16
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Ground Control hosting and extension points

## Context

Plan item 9 (extension points) found these limits at `508b0d6`:

* **Ground Control runs standalone on Harbor 2.x.** The Harbor 3 extension API it would plug into does not exist yet.
* **Integrators get no fleet events.** Changes to satellites, groups and configs, and what satellites report (ADR-0015's edge events), stay inside Ground Control.
* **The outbox only grows.** Done events of `event_outbox` (ADR-0014) are never deleted, and the journal's log-only edge events will make up most of its rows.
* **The satellite's OCI proxy has no in-process seam** for admission, authentication or metrics.

## Decision Drivers

* An integrator receives every event at least once and in order, can deduplicate it, and can verify it came from Ground Control.
* A dead receiver can neither block other receivers nor keep the outbox from shrinking.
* The payloads are a public contract that only grows.

## Decision

1. **Hosting (G2).** Ground Control stays standalone on `adapters/harbor2`. Extension mode, a `harbor3` adapter fed by `fleet.RegistryEventSource` (9.5), is deferred: the port and its fake exist, with no adapter and no consumer until the API does. Follow-up: the `/groups/sync` 404 from the `harbor-satellite` replication adapter.
2. **Webhooks (9.1).**
   * `event_outbox` is read as an ordered log by `(xid, id)` (index added by migration 022) with one checkpoint per subscription in `webhook_checkpoints`. Reads stop below the current xmin, so a checkpoint never passes a row that commits later with a lower xid. A new subscription starts at the current xmin. The xmin is the server's: any open transaction on the Postgres server, in any database, holds delivery to every subscription back for as long as it runs. 9.1 therefore exports the delivery lag, the age of the oldest undelivered event per subscription, as a metric.
   * Delivery is at least once and in order: a failure stops that subscription until its backoff and nothing is skipped. Filtered kinds advance the checkpoint without a send. One worker delivers a subscription at a time (two-key advisory lock `WebhookSubscriptions`), with no transaction open across a delivery.
   * A subscription failing without a single delivery for `WEBHOOK_DISABLE_AFTER` is disabled; deleting and recreating it resumes.
   * Retention (`OUTBOX_RETENTION`) deletes done events but never above an enabled checkpoint. It runs whether or not anyone subscribes, because it is the only retention of the edge events: about one row per replicated artifact, pull-through fill and CRI change per satellite, kept for `OUTBOX_RETENTION`.
   * The sender follows no redirects, requires https except to loopback and dials no link-local address (169.254.0.0/16, fe80::/10), which covers the cloud metadata endpoints. Private ranges stay allowed, because receivers usually live there, and only system admins create subscriptions.
   * `WEBHOOKS_ENABLED` gates the API and the dispatcher; secrets are sealed with `DATA_ENCRYPTION_KEY`.
3. **CloudEvents and catalog (9.2).** CloudEvents 1.0, HMAC-SHA256 over `<timestamp>.<body>` in `X-Ground-Control-Signature-256` (ADR-0011 convention), the outbox id as the event id and idempotency key. `spec/events/v1` holds one JSON Schema per kind, generated from `fleet.EventPayloads()` by `test/contract` (`-update`) and checked against it there; v1 changes are additive only. The type is `io.harborsatellite.groundcontrol.<Kind>`, or ADR-0011's type where ADR-0015 maps one.
4. **Edge events.** ADR-0015 owns their emission (9.4), their payloads (`edgejournal`) and the ADR-0011 type mapping; this record only delivers them.
5. **Edge seam (9.3).** `cmd/satellite` composes the proxy with `middleware.Stack` and `WrapAll`: metrics outermost, then authentication, then admission (ADR-0017), then the pull handler. All slots are nil today. Authentication of non-loopback clients is required before the proxy listens beyond loopback (G7).
6. **Outbox evolution (follow-up, not in this skeleton).** The table-polled log above stays while the floor is PostgreSQL 14 and the driver lib/pq.
   * **Now, on 14:** the per-plane `DB_{API,FLEET,ASYNC}_STATEMENT_TIMEOUT` and `_IDLE_IN_TX_SESSION_TIMEOUT` (parsed, applied by plan item 3) get non-zero defaults. They bound the xmin stall caused by Ground Control's own sessions, not by other clients of a shared server.
   * **Time partitioning, on 14:** `event_outbox` partitioned by `created_at`, retention by dropping a partition once every enabled checkpoint is past it instead of `PruneDoneOutboxEvents`. This removes the DELETE churn and most bloat; the primary key then includes `created_at`.
   * **WAL reader, with a floor of 17 (preferably 18):** the ordered readers (webhooks, edge events) move to logical decoding (`pgoutput` with `messages`) on one slot, and log-only kinds are written with `pg_logical_emit_message(true, …)` instead of rows inserted already done. Delivery then follows commit order, the cluster-wide xmin guard goes away (a logical slot holds back only `catalog_xmin`), and checkpoints become LSNs, with the slot confirmed at the lowest enabled checkpoint. It waits for 17 because earlier releases lose logical slots on failover and on `pg_upgrade`; 17 adds failover slots (`sync_replication_slots`, `synchronized_standby_slots`), keeps slots across `pg_upgrade` from 17 on, and adds `transaction_timeout`. 18 adds `idle_replication_slot_timeout`, so a dead dispatcher cannot pin WAL until the disk fills; on 17 `max_slot_wal_keep_size` must be set instead. It needs `wal_level=logical` (a restart, on managed services a parameter change) and a replication-protocol client: lib/pq has none, so that connection uses pgx with `pglogrepl`.
   * **Unchanged by the WAL reader:** the publisher's work queue. Leases, attempts and release while the registry is down need the claim table; the stream gives order and wake-ups, not retries.
   * **19 (RC1 on 2026-10-15, not final):** enables logical decoding on demand when `wal_level=replica`, without a restart, which removes the operator step. It also makes NOTIFY wake only the backends that listen, so LISTEN/NOTIFY could replace the 1s poll of the publisher and dispatcher, and adds `REPACK (CONCURRENTLY)` for outbox bloat. Re-check against the final release notes.

## Consequences

* Good: integrators get ordered, signed, deduplicable fleet and edge events; the outbox finally has retention.
* Bad: migration 022 builds the `(xid, id)` index inside goose's transaction (no `CONCURRENTLY`), which blocks writes to `event_outbox` during the build on upgrade. Before this release the outbox has no retention, so the pause grows with the install's age; afterwards retention keeps the table small.
* Bad: a long transaction anywhere on the Postgres server delays every webhook; the delivery-lag metric shows it.
* Bad: a receiver that is down longer than `WEBHOOK_DISABLE_AFTER` loses events until it is recreated.
* Neutral: extension mode waits for Harbor 3.
* Neutral: removing the xmin guard and the polling churn (Decision 6) is tied to raising the PostgreSQL floor to 17 and adding pgx for one connection.

## Implementation Status

| Part | Status |
|---|---|
| ADR | proposed |
| Migration `022_webhooks.sql`, webhook and outbox-log queries | done, SQL contract tests |
| `internal/groundcontrol/webhook` (CloudEvents, signer, sender, dispatcher), `fleet.WebhookService`, postgres `WebhookRepo`, `WebhookStore`, `OutboxLog`, `SubscriptionLocker`, `OutboxStore.Prune`, `async.OutboxPruner` | skeleton, pending tests; `EventType` written |
| `/api/webhooks` routes | in the spec and `PolicyRoutes`, system admin only; handlers answer 501 |
| `spec/events/v1`, `fleet.EventPayloads` | done, contract tests |
| Delivery-lag metric | not started (9.1) |
| `fleet.RegistryEventSource` | port and fake only |
| `internal/satellite/proxy/middleware` | `Stack` done and wired with nil slots; `Authenticate` and `Metrics` stubs |
| Outbox evolution (Decision 6): timeout defaults, time partitioning, WAL reader | not started |

## Validation

* The pending tests of plan item 9 pass without their markers, among them `TestDispatcher_ExactlyOncePerEventIDAcrossGracefulRestart` and `TestOutboxPruner_FailingSubscriptionStopsHoldingRowsOnceDisabled`.
* Webhook E2E: `GroupChanged` and `SatelliteStale` delivered exactly once per event id across a Ground Control restart.
