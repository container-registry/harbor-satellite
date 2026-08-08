---
status: proposed
date: 2026-08-05
deciders: [Harbor Satellite Development Team]
consulted: [Harbor Satellite Users, Operators]
informed: [Harbor Satellite Developers]
---

# Satellite Eventing System

Tracks: [#117](https://github.com/container-registry/harbor-satellite/issues/117), [#58](https://github.com/container-registry/harbor-satellite/issues/58)

## Context and Problem Statement

Operators who embed Harbor Satellite in a larger pipeline need to react to what a
satellite is doing — a new artifact synchronized, a sync cycle failed, connectivity
degraded — without polling logs or the Ground Control API. Issue #117 asks for an
ADR precisely because the requirements are open-ended: rate-limit-aware transfer,
connectivity-based transfer gating, and triggering a downstream deployment were all
raised as candidate use cases, and they share no obvious contract. Issue #58 is more
concrete: it lists Event Triggers (new state, sync started/completed/failed, artifact
synchronized) and Actions (local CLI/shell/container, REST), and points at
[CloudEvents](https://cloudevents.io/) / [CDEvents](https://cdevents.dev/) as
candidate formats.

Satellite already ships one production event pipeline: the audit logger
(`internal/logger/audit.go`), which fans a fixed-schema `AuditEvent` out to
pluggable `Transport`s (syslog, OTel) for security-relevant actions (login, ZTR,
config change). It is not a fit for operational lifecycle signals — its
`Operation`/`ResourceType`/`Outcome` vocabulary is deliberately closed
(`audit.go:46-71`) and its purpose is a compliance trail, not a general
publish/subscribe mechanism a workflow engine subscribes to.

A maintainer raised a scoping concern directly on #117: building a full
downstream-eventing/workflow system risks duplicating what CD platforms already
solve, and Satellite's job is better framed as an edge proxy registry delivering
artifacts, not an orchestrator. That concern is treated as a hard constraint below:
this ADR scopes Satellite to **emitting** typed lifecycle events, not consuming,
routing, or acting on them.

## Decision Drivers

* Must not duplicate or overload the audit logger's fixed security-event schema.
* Must reuse the `Transport`/`Reconfigure` pattern already proven in production
  (`internal/logger/audit.go`) rather than invent a new plugin shape.
* Must not block the replication critical path
  (`FetchAndReplicateStateProcess.Execute`, `internal/satellite/state/state_process.go:79`)
  — event emission is best-effort and asynchronous.
* Must not turn Satellite into a workflow engine or CD platform (maintainer
  constraint from the #117 discussion) — Satellite emits, downstream systems decide
  and act.
* Output format should be consumable by generic eventing platforms (Argo Events,
  Knative Eventing, or a plain webhook receiver) without bespoke integration work.
* Executing local commands/containers as a reaction to a remote-influenced signal
  (#58's "Local CLI/Shell/Container" action) is a code-execution risk and needs its
  own threat model before it ships.
* Config must hot-reload like every other `AppConfig` section.
* Every exit path of `Execute()` must be observable, not just the aggregate
  error from `collectResults` — an early return (context cancellation, a
  root-state fetch failure) is a real, common failure mode and must not
  silently produce no event.
* Delivery is best-effort, not exactly-once: a full queue can drop an event,
  and a webhook retry can redeliver one. The design must say so explicitly
  rather than imply a stronger guarantee than the queue/retry model provides.

## Considered Options

* Option 1: Extend the existing `AuditLogger`/`AuditEvent` to also carry
  operational lifecycle events.
* Option 2: New sibling event system (`internal/eventing`) reusing the audit
  logger's `Transport`/`Reconfigure` pattern, with its own typed event vocabulary
  and a CloudEvents-shaped wire format, emitted over an HTTP webhook transport.
* Option 3: Adopt an external message broker (NATS/Kafka) as a new satellite
  dependency.
* Option 4: No structured events; consumers scrape structured `zerolog` output.

## Decision Outcome

Chosen option: "New sibling event system emitting CloudEvents over HTTP webhooks",
because it reuses a pattern already validated in production, keeps the audit
trail's schema closed and compliance-focused, needs no new satellite dependency or
long-running broker process, and produces a wire format every mainstream eventing
consumer already understands.

### Consequences

* Good, because it reuses the `Transport`/`Reconfigure`/hot-reload shape from the
  audit logger — no new architectural concept needs review.
* Good, because CloudEvents JSON means Argo Events, Knative Eventing, or a
  five-line webhook receiver can consume events with no Satellite-specific client.
* Good, because emission is decoupled from Satellite deciding what to do about
  network conditions or rate limits — those stay separate features (#63) that may
  consume these events later.
* Neutral, because the first transport is HTTP webhook only; syslog/OTel event
  transports and the local CLI/shell/container action from #58 are deferred (see
  Future Work) pending a security review of arbitrary local execution.
* Neutral, because per-artifact events require threading a reporter through
  `BasicReplicator.Replicate` (`internal/satellite/state/replicator.go:107-177`),
  which today only returns one aggregate error for the whole batch — a small
  interface change, not a rewrite.
* Bad, because a second best-effort delivery pipeline (webhook) is now part of
  Satellite's runtime surface, with its own retry/backoff/queue-depth failure
  modes to operate and test.

## Scope

In scope:

* Emitting typed lifecycle events for: sync started, sync completed, sync failed,
  new state received, artifact synchronized, artifact deleted, config updated.
* One transport: HTTP webhook, CloudEvents v1.0 JSON, HMAC-signed.
* Config: `EventingConfig` under `AppConfig`, hot-reloadable, following the
  existing `AuditConfig` shape.

Out of scope (see Future Work):

* Any built-in action/workflow execution (shell, container, CD trigger) —
  Satellite emits, it does not act.
* Rate limiting or connectivity-aware transfer gating (#63) — a separate feature
  that could subscribe to these events.
* Ground Control-side events (this ADR covers the satellite process only; GC
  already has its own audit trail).
* syslog/OTel transports for these events (the audit logger already owns those
  destinations for security events; revisit if operators ask for one unified
  delivery pipeline instead of two).

## Architecture

Events are emitted from the same points the replication cycle already passes
through today — no new control flow is introduced, only observation points.

```text
┌───────────────────────────┐
│      Scheduler tick        │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Execute(ctx) (err error): start()         │
│ state_process.go:79-81                    │──emit──▶ ((sync.started))
│ deferred emit of sync.completed/failed    │
│ covers every return below, not just       │
│ collectResults (see note)                 │
└─────────────┬───────────────────────────────┘
              │
              ▼
      ┌────────────────┐   ctx cancelled (:87-91)
      │  ctx.Done()?    │─────────────────────────▶ return ctx.Err()
      └───────┬─────────┘
              │ not yet
              ▼
      ┌────────────────┐   missing creds (:95-99)
      │  CanExecute?    │─────────────────────────▶ return nil, no event
      └───────┬─────────┘
              │ ok
              ▼
┌─────────────────────────────────────────┐
│ fetchSatelliteRootState /                 │
│ Harbor-URL override                       │──emit──▶ ((state.received))
│ state_process.go:102-114                  │──error──▶ return err
└─────────────┬───────────────────────────────┘
              │ ok
              ▼
┌─────────────────────────────────────────┐
│ processGroupState per group               │
│ state_process.go:130-135                  │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ GetChanges diff                           │
│ state_process.go:179-227                  │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Replicator.Replicate /                    │
│ DeleteReplicationEntity                   │──emit──▶ ((artifact.synchronized
│ replicator.go:107-177                     │           / artifact.deleted))
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ collectResults                            │
│ state_process.go:273-327                  │
└─────────────┬───────────────────────────────┘
              │
              ▼
     Execute(ctx) returns (nil or err) ◀── ctx-cancel / fetch-error
              │                             returns above also land here
       ┌──────┴───────┐
   nil err           non-nil err
       │                │
       ▼                ▼
((sync.completed))  ((sync.failed))

  the deferred emit fires exactly once per Execute() call, on every
  return path except the "missing creds" one (which is not a failure,
  just not-yet-ready, and intentionally emits nothing)
```

Delivery is decoupled from the sync cycle by a bounded queue: the process emits
into the queue and continues immediately, a single dispatcher goroutine per
transport drains it, and a full queue drops the event (logged and counted) rather
than applying backpressure to replication.

**Delivery is best-effort, not exactly-once.** A full queue drops an event
(lost). A webhook retry after a delivered-but-unacknowledged POST redelivers
the same event (duplicated). Satellite does not attempt durable, exactly-once
delivery — CloudEvents itself permits redelivery of the same event. Each event
is assigned one `id` at emission time, and a retry reuses that `id` rather than
minting a new one, so receivers can deduplicate by `id` if they need to.

```text
┌────────────┐        ┌─────────────────────────────────┐
│ Scheduler   │───────▶│ FetchAndReplicateStateProcess    │
└────────────┘        │ Execute(ctx)                     │
                       └────────────────┬──────────────────┘
                                        │ Emit(sync.started)
                                        │ Emit(artifact.synchronized) x N
                                        │ Emit(sync.completed | sync.failed)
                                        ▼
                       ┌─────────────────────────────────┐
                       │ internal/eventing Emitter         │
                       │ (bounded queue, non-blocking)      │
                       └────────────────┬──────────────────┘
                                        │ dispatcher goroutine drains queue
                                        ▼
                       ┌─────────────────────────────────┐
                       │ Webhook Transport                  │
                       └────────────────┬──────────────────┘
                                        │ POST CloudEvents JSON
                                        │ (HMAC-signed, same id on retry)
                                        ▼
                       ┌─────────────────────────────────┐
                       │ External consumer                   │
                       │ (Argo Events / Knative / custom)    │
                       └────────────────┬──────────────────┘
                                        │ 2xx, or non-2xx (logged, bounded
                                        │ retry — may duplicate on receiver)
                                        ▼
                                (delivery result)
```

### Event Catalog (Phase 1)

| Event type | Trigger | Source | Payload highlights |
|---|---|---|---|
| `io.harborsatellite.state.sync.started` | `Execute()` begins, `CanExecute` passed | `state_process.go:79-99` | `satellite_id`, `cycle_id`, `schema_version` |
| `io.harborsatellite.state.received` | Root or group state artifact fetched | `state_process.go:102`, `:448` | `cycle_id`, `group`, `digest`, `artifact_count`, `schema_version` |
| `io.harborsatellite.artifact.synchronized` | Entity replicated | `replicator.go:152-173` | `cycle_id`, `group`, `repository`, `tag`, `digest`, `bytes`, `duration_ms`, `schema_version` |
| `io.harborsatellite.artifact.deleted` | Entity removed | `state_process.go:459` | `cycle_id`, `group`, `repository`, `tag`, `digest`, `schema_version` |
| `io.harborsatellite.config.updated` | Remote config digest changed | `state_process.go:329-411` | `cycle_id`, `digest_old`, `digest_new`, `schema_version` |
| `io.harborsatellite.state.sync.completed` | `Execute()` returns nil, via a deferred emit (only reachable through `collectResults`) | `state_process.go:273-327` | `cycle_id`, `duration_ms`, `groups_synced`, `schema_version` |
| `io.harborsatellite.state.sync.failed` | `Execute()` returns a non-nil error, via a deferred emit — covers context cancellation and root-state/Harbor-override fetch errors, not just `collectResults`'s aggregate error | `state_process.go:87-91`, `:102-114`, `:273-327` | `cycle_id`, `error`, `groups_failed` (omitted for early-return errors), `schema_version` |

### Event Payload Schema and Correlation

Every event's `Data` payload carries `cycle_id` — including the non-sync events
(`state.received`, `artifact.*`, `config.updated`) — so a consumer can group
everything that happened during one replication cycle back together after
async, possibly reordered delivery, not just correlate the two sync bookend
events.

Every payload also carries `schema_version` (starting at `"1"`). Changes within
a version must be additive only (new optional fields); a breaking change to an
existing event's fields mints a new event `type` (e.g. a `.v2` suffix) rather
than silently changing what existing consumers of the `v1` type receive. Field
types, which fields are required, and per-field size limits are implementation
detail to be pinned down in the PR that adds `internal/eventing`, not in this
ADR — the schema-evolution rule above is what's being decided here.

### Emitter and Transport shape

Mirrors `internal/logger/audit.go`'s `Transport` interface, with a context so a
webhook POST respects cancellation on shutdown. `Emitter` is the sibling of
`AuditLogger`: it owns the bounded queue and dispatcher goroutine, and exposes
`Reconfigure` the same way `AuditLogger.Reconfigure` does (`audit.go:254-269`)
— build and verify the new `Transport` up front, swap it under a lock, close
the old one after. The queue itself is untouched by a reconfigure: events
already enqueued are delivered via whichever `Transport` is active when the
dispatcher goroutine dequeues them, so a reload mid-cycle can route a small
tail of in-flight events to the old transport. That's acceptable given
delivery is already best-effort (see above).

```go
// internal/eventing/event.go (proposed)
type Event struct {
    ID     string         // CloudEvents "id" — unique per emission, reused on retry
    Source string         // CloudEvents "source" (URI-reference): satellite name or SPIFFE ID
    Type   string         // CloudEvents "type", e.g. "io.harborsatellite.artifact.synchronized"
    Time   time.Time      // CloudEvents "time"
    Data   map[string]any // CloudEvents "data"; always includes cycle_id, schema_version
}

// The webhook transport serializes Event as CloudEvents v1.0 structured JSON
// (specversion "1.0" added at encode time, not stored on Event) with
// Content-Type: application/cloudevents+json.

type Transport interface {
    Emit(ctx context.Context, e Event) error
    Close() error
}

type Emitter struct { /* bounded queue, dispatcher goroutine(s), current Transport */ }

func (e *Emitter) Reconfigure(cfg EventingConfig) error // mirrors AuditLogger.Reconfigure
```

### Webhook Transport Security

* **Signing**: HMAC-SHA256 over the raw JSON request body, sent as
  `X-Satellite-Signature-256: sha256=<hex>` (same convention as GitHub
  webhooks).
* **Replay protection**: an `X-Satellite-Timestamp` header (Unix seconds) is
  included in the signed material (HMAC computed over `timestamp + "." +
  body`, not the body alone). Receivers should reject requests outside a
  5-minute window; a captured signed payload can't be replayed indefinitely.
* **Transport**: HTTPS is required for any non-loopback URL; the webhook
  transport refuses to start with a plain `http://` non-loopback URL, mirroring
  how this project already gates other insecure options (e.g. `USE_UNSECURE`).
* **Redirects**: the HTTP client does not follow redirects — an
  attacker-controlled 3xx response redirecting a signed payload to a different
  host is refused, not followed.
* **Secret rotation**: `signing_secret_env` names one env var; rotating the
  secret today means reloading with the new value (a hard cutover). Accepting
  two active secrets during a rotation window is Future Work, not Phase 1.

### Config shape

Slots into `AppConfig` next to `Audit`, following the existing
`AuditConfig`/`SyslogAudit`/`OtelAudit` nesting (`pkg/config/config.go:190-207`).
`eventing.enabled` is the master switch, exactly like `AuditConfig.Enabled`:
when `false`, the `Emitter` is a no-op regardless of `webhook.enabled`, so an
operator can keep the webhook config on file and toggle all of eventing off
with one flag:

```json
"eventing": {
  "enabled": false,
  "webhook": {
    "enabled": true,
    "url": "https://example.com/hooks/harbor-satellite",
    "timeout": "5s",
    "signing_secret_env": "SATELLITE_EVENT_SIGNING_SECRET",
    "queue_size": 256
  }
}
```

`GetEventingConfig()` / `SetEventingConfig(...)` follow the same one-liner
getter/modifier pattern as `GetAuditConfig()` (`pkg/config/getters.go:208`) and
`SetDirectDelivery` (`pkg/config/modifiers.go:121`).

## Validation

* `sync.started`/`sync.completed`/`sync.failed` are each attempted exactly once
  per `Execute()` call (one deferred emit per call), even with concurrent
  per-group goroutines (`go test -race`) — but see the best-effort note below
  for what happens between attempt and receiver.
* `sync.failed` fires on every non-nil return from `Execute()`, not just
  `collectResults`'s aggregate error: add a test that forces a context
  cancellation and a `fetchSatelliteRootState` error and asserts both emit
  `sync.failed`.
* Delivery is best-effort, not exactly-once: a soak test against a receiver
  that intermittently 5xxs must show duplicate deliveries share the same event
  `id`, and a soak test against a sustained-down receiver must show dropped
  events are logged and counted with memory staying bounded.
* Emission never blocks `Execute()`: a deliberately slow or hung webhook receiver
  must not measurably change replication cycle latency beyond the bounded enqueue.
* HMAC signature verified end-to-end against a reference receiver, including
  that a request outside the replay window (stale `X-Satellite-Timestamp`) is
  rejected and a 3xx response from the receiver is not followed.
* CloudEvents JSON validated against the CloudEvents SDK conformance checks,
  including `specversion: "1.0"` and `Content-Type: application/cloudevents+json`.
* Hot-reload: toggling `eventing.enabled` or changing the webhook URL takes effect
  without restart, mirroring `AuditConfig.Reconfigure` (`audit.go:254-269`); events
  enqueued immediately before a reload may still deliver via the old transport.

## Future Work

* Additional transports (syslog, OTel event export, message brokers) if operators
  want one delivery pipeline instead of two.
* An inbound control channel (a downstream system telling Satellite to throttle)
  is explicitly out of scope here — this ADR is outbound-only. It would likely
  piggyback on the existing remote config mechanism ([ADR-0003](0003-remote-config-injection.md))
  rather than a new channel.
* Local CLI/shell/container action execution (#58) — deferred pending a dedicated
  threat model; if built, it must be opt-in, off by default, and restricted to an
  operator-defined command allowlist rather than dynamic, remotely-influenced
  commands.
* Rate limiting / connectivity-aware transfer gating (#63) as a consumer of
  `artifact.synchronized`'s `bytes`/`duration_ms` payload, not as part of this ADR.
* Ground Control-side event emission (admin actions, satellite lifecycle
  transitions per [ADR-0006](0006-satellite-lifecycle-states.md)) as a parallel,
  separate ADR if requested.
* Coordination with [ADR-0009](0009-transparent-oci-registry-proxy.md)'s proxy
  admission decisions (allow/deny) as additional event types, once that ADR moves
  past `proposed`.

## References

* [Issue #117 - ADR Entry for Eventing System](https://github.com/container-registry/harbor-satellite/issues/117)
* [Issue #58 - Satellite Downstream Notifications](https://github.com/container-registry/harbor-satellite/issues/58)
* [Issue #63 - Track and report upstream how many bytes were transferred](https://github.com/container-registry/harbor-satellite/issues/63) (related, not implemented here)
* [CloudEvents Specification](https://cloudevents.io/)
* [CDEvents](https://cdevents.dev/)
* `internal/logger/audit.go` - existing `Transport`/`Reconfigure` precedent
* [ADR-0003](0003-remote-config-injection.md) - Remote Config Injection
* [ADR-0006](0006-satellite-lifecycle-states.md) - Satellite Lifecycle States
* [ADR-0009](0009-transparent-oci-registry-proxy.md) - Transparent OCI Registry Proxy
