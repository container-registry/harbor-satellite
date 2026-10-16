# Ground Control event catalog

Ground Control delivers fleet events to webhook subscriptions as
[CloudEvents 1.0](https://github.com/cloudevents/spec) (plan item 9.2,
ADR-0019). `v1/<Kind>.json` is the JSON Schema (2020-12) of each event's
`data`, one file per `fleet.EventKinds()`, generated from
`fleet.EventPayloads()` with
`go test ./test/contract -run TestEventCatalog_MatchesPayloadTypes -update`. Within v1 fields are only added,
so receivers must accept unknown fields.

## Type naming

The CloudEvents `type` is `io.harborsatellite.groundcontrol.<Kind>`, except
for the edge kinds ADR-0015 maps to an ADR-0011 type:

| Kind | Type |
|---|---|
| `EdgeReplicated` | `io.harborsatellite.artifact.synchronized` |
| `EdgeDeleted` | `io.harborsatellite.artifact.deleted` |

`dataschema` is `https://satellite.container-registry.com/schemas/events/v1/<Kind>.json`.
Edge events carry the record's `internal/shared/edgejournal` payload, with
`name`, `epoch`, `seq` and `recorded_at` of its origin at the same level.

## Signing

Each request carries `X-Ground-Control-Timestamp` (Unix seconds) and
`X-Ground-Control-Signature-256: sha256=<hex>`, the HMAC-SHA256 of
`<timestamp>.<body>` with the subscription's secret. Receivers should reject
stale timestamps.

## Delivery

Delivery is at least once, in outbox order per subscription. The event `id`
is the outbox id: use it as the idempotency key. A subscription that fails
without a single delivery for `WEBHOOK_DISABLE_AFTER` is disabled; delete and
recreate it to resume. A new subscription starts at the server's current
xmin: events of transactions that finished before it are skipped, those of
transactions still open are delivered. Any long transaction on the Postgres
server holds delivery back until it ends.

## Retention

Done events are pruned after `OUTBOX_RETENTION` (default 168h), but never
above the checkpoint of an enabled subscription. Edge events, which make up
most of the outbox, are pruned after `OUTBOX_RETENTION` even when nobody
subscribes.
