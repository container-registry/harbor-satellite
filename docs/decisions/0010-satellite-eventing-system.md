---
status: proposed
date: 2026-08-05
deciders: [Harbor Satellite Development Team]
consulted: [Harbor Satellite Users, Operators]
informed: [Harbor Satellite Developers]
---

# Satellite Eventing System,

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

```
┌───────────────────────────┐
│      Scheduler tick        │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Execute(): start()                        │
│ state_process.go:79-81                    │──emit──▶ ((sync.started))
└─────────────┬───────────────────────────────┘
              │
              ▼
      ┌───────────────┐        missing creds        ┌────────────────────┐
      │  CanExecute?   │────────────────────────────▶│  stop, no event     │
      └───────┬────────┘                              └────────────────────┘
              │ ok
              ▼
┌─────────────────────────────────────────┐
│ fetchSatelliteRootState                   │
│ state_process.go:102                      │──emit──▶ ((state.received))
└─────────────┬───────────────────────────────┘
              │
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
       ┌──────┴───────┐
   no error         error
       │                │
       ▼                ▼
((sync.completed))  ((sync.failed))
```

Delivery is decoupled from the sync cycle by a bounded queue: the process emits
into the queue and continues immediately, a single dispatcher goroutine per
transport drains it, and a full queue drops the event (logged and counted) rather
than applying backpressure to replication.

```
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
                                        │ (HMAC-signed)
                                        ▼
                       ┌─────────────────────────────────┐
                       │ External consumer                   │
                       │ (Argo Events / Knative / custom)    │
                       └────────────────┬──────────────────┘
                                        │ 2xx, or non-2xx
                                        │ (logged, bounded retry)
                                        ▼
                                (delivery result)
```

### Event Catalog (Phase 1)

| Event type | Trigger | Source | Payload highlights |
|---|---|---|---|
| `io.harborsatellite.state.sync.started` | `Execute()` begins, `CanExecute` passed | `state_process.go:79-99` | `satellite_id`, `cycle_id` |
| `io.harborsatellite.state.received` | Root or group state artifact fetched | `state_process.go:102`, `:448` | `group`, `digest`, `artifact_count` |
| `io.harborsatellite.artifact.synchronized` | Entity replicated | `replicator.go:152-173` | `group`, `repository`, `tag`, `digest`, `bytes`, `duration_ms` |
| `io.harborsatellite.artifact.deleted` | Entity removed | `state_process.go:459` | `group`, `repository`, `tag`, `digest` |
| `io.harborsatellite.config.updated` | Remote config digest changed | `state_process.go:329-411` | `digest_old`, `digest_new` |
| `io.harborsatellite.state.sync.completed` | `collectResults` returns nil | `state_process.go:273-327` | `cycle_id`, `duration_ms`, `groups_synced` |
| `io.harborsatellite.state.sync.failed` | `collectResults` returns an error | `state_process.go:273-327` | `cycle_id`, `error`, `groups_failed` |

### Emitter and Transport shape

Mirrors `internal/logger/audit.go`'s `Transport` interface, with a context so a
webhook POST respects cancellation on shutdown:

```go
// internal/eventing/event.go (proposed)
type Event struct {
    ID     string         // CloudEvents "id"
    Source string         // CloudEvents "source": satellite name or SPIFFE ID
    Type   string         // CloudEvents "type", e.g. "io.harborsatellite.artifact.synchronized"
    Time   time.Time
    Data   map[string]any // CloudEvents "data"
}

type Transport interface {
    Emit(ctx context.Context, e Event) error
    Close() error
}
```

### Config shape

Slots into `AppConfig` next to `Audit`, following the existing
`AuditConfig`/`SyslogAudit`/`OtelAudit` nesting (`pkg/config/config.go:190-207`):

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

* Event emitted exactly once per cycle for `sync.started`/`sync.completed`/
  `sync.failed`, even with concurrent per-group goroutines (`go test -race`).
* Emission never blocks `Execute()`: a deliberately slow or hung webhook receiver
  must not measurably change replication cycle latency beyond the bounded enqueue.
* Queue-full behavior: dropped events are logged and counted, memory stays
  bounded under a sustained-down receiver (soak test).
* HMAC signature verified end-to-end against a reference receiver.
* CloudEvents JSON validated against the CloudEvents SDK conformance checks.
* Hot-reload: toggling `eventing.enabled` or changing the webhook URL takes effect
  without restart, mirroring `AuditConfig.Reconfigure` (`audit.go:254-269`).

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
