---
status: proposed
date: 2026-10-15
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Edge journal and upload protocol

## Context

Plan item 6 (edge cold path) found these limits at `508b0d6`:

* **Heartbeats carry a snapshot only.** The status report (`internal/satellite/state/report.go`) sends CPU, memory, storage, image count and CRI activity. Replication outcomes per artifact and pull-through events are not reported, and a heartbeat that fails is lost: the next one carries a new snapshot. `latest_state_digest`, `latest_config_digest` and `last_sync_duration_ms` are declared (`report.go:22,23,28`) but never set.
* **Ground Control never learns what replicated.** Nothing reports which artifacts a group replicated or deleted. A restart repeats a group's replication, which stays cheap because oras skips content that already exists (oras-go v2.6.2 `copy.go:228-239`).
* **Every tick pulls the satellite state and every group state** with `crane.Pull` (`state/state_process.go:116,465`, `state/fetcher.go:129`). Only the config is resolved with one HEAD first (`state_process.go:368`).
* **Group replication is unbounded.** Each group replicates in its own goroutine (`state_process.go:145`), with no bandwidth budget toward Harbor. Replication reuses the Harbor store the proxy pulls through, so a budget on that store would also slow the proxy.
* **The registry client drops retries with TLS.** `registryHTTPClient` replaces oras's retrying transport with a plain one as soon as a TLS option is set (`store/registry.go:270-273`; 275-278 on the skeleton branch).
* **Event schedulers outlive the satellite.** They are registered with `context.Background()` (`satellite.go:120`) and `Satellite.Stop` (`:144`) never stops them; `Scheduler.Stop` does not end a run loop either. `Scheduler.Trigger` is an unbuffered send (`scheduler/scheduler.go:105-107`) that the heartbeat calls for every event (`state/reporting_process.go:133`), so it blocks the heartbeat until the event loop receives, and forever once that loop has exited. `EventScheduler.SendEvent` reads its map without the lock `Register` holds.
* **Open PR #597 proposes ADR-0011, satellite eventing.** The satellite emits CloudEvents (`io.harborsatellite.state.sync.*`, `io.harborsatellite.artifact.synchronized`, `io.harborsatellite.artifact.deleted`, `io.harborsatellite.config.updated`, each with `cycle_id` and `schema_version`) to an HMAC-signed webhook through a bounded in-memory queue. Delivery is best effort: a full queue drops events and retries may duplicate them. It covers the satellite process only and names "one delivery pipeline instead of two" as future work.

## Decision Drivers

* An edge event reaches Ground Control at least once, across restarts, crashes and offline periods.
* A heartbeat stays one request; no second channel and no broker. A heartbeat without records stays the cheap one.
* Ground Control deduplicates uploads and acknowledges them without scanning a satellite's history, and survives losing its own cursor.
* Disk use at the edge is bounded, records lost at the bound are visible, and recording an event never slows a pull.
* The replication cold path takes no bandwidth from the proxy's hot path.

## Decision

1. **Vocabulary.** `internal/shared/edgejournal` holds the record types and their payloads for the satellite and Ground Control alike. Every payload carries `schema_version`; within a version fields are only added.
   * `replication`: one artifact replicated into, or deleted from, the local store. It has `action` (`replicated` or `deleted`) and ADR-0011's `cycle_id`, `group`, `repository`, `tag`, `digest`, `bytes` and `duration_ms`, plus `error` when the action failed.
   * `cri`: one container runtime configured to pull through the satellite.
   * `pull_through`: a proxy miss filled from Harbor.
   * `dropped`: how many records the journal dropped under backpressure instead of writing them.

   Payloads are frozen by golden JSON tests. Status reports are not journaled; `satellite_status` already holds them.
2. **Journal (6.4).** `internal/satellite/journal` appends records to segment files `seg-<first seq>.log` in `<config dir>/journal`.
   * **Format.** Length-prefixed JSON with a CRC32C per record. The directory is synced when a segment is created or deleted, and the epoch and ack files are replaced atomically. Opening the journal cuts a torn record off the tail.
   * **Group commit.** Concurrent `Append` calls share one sync, gathered for at most `Options.SyncInterval`.
   * **Pull-through.** The pull-through producer uses `TryAppend`, which never blocks. `Options.QueueSize` bounds the records accepted but not yet synced; slots are released after each sync. Beyond it records are dropped and get no seq. Once a slot is free, the writer appends one `dropped` record with their count. Seqs therefore stay contiguous, and the loss reaches Ground Control.
   * **Seqs and epochs.** Seqs are int64 and increase without holes within the journal's epoch. Epochs are time-ordered: an epoch is drawn as the time in Unix milliseconds when the directory is created, and kept across restarts. Two events move it forward. An answer naming a newer Ground Control epoch makes the journal continue as that epoch plus 1. An ack above the journal's last persisted seq means the directory was restored from a snapshot, and the journal draws a new epoch above the current one.
   * **Floor.** The floor is the ack, raised past records the journal gave up (the cap below, or `Drop`). It never decreases, including when the epoch moves, so records the satellite gave up are never uploaded as if they still existed.
3. **Cap.** `app_config.journal` sets `max_segment_bytes` (default 4 MiB) and `max_total_bytes` (64 MiB). Beyond the total the oldest unacknowledged records are dropped and the floor rises past them.
4. **Upload (6.5).** The heartbeat (`POST /satellites/sync`) carries `journal {epoch, after_seq, records}`.
   * **Request.** `after_seq` is the journal's floor. `records` are the records right above it, oldest first, at most `upload_batch` (default 500, at most 1000).
   * **Body limit.** `SYNC_MAX_BODY_BYTES` (default 4 MiB) bounds the request body; a larger one is answered 413.
   * **Response.** Whenever the request carried a journal, the response carries `journal {epoch, acked_seq, error}`.
   * **When to send.** Until a satellite has seen that object, it sends one record per heartbeat, so a Ground Control that ignores journals costs one record per beat. A beat with nothing above the last ack and no raised floor carries no journal.
5. **Ingest.** `fleet.JournalService.Ingest` runs inside the heartbeat service (plan item 4): `Beat` records the heartbeat first and then ingests, so a journal never makes a satellite stale.
   * **Validation.** Ingest validates the envelope only, before any statement. An epoch below 1, a negative floor, more than 1000 records, a seq below 1, or records that do not run on from `after_seq + 1` one by one is answered with `error`, `acked_seq` = `after_seq` and HTTP 200.
   * **Records it cannot map.** A record of a type, action or payload Ground Control does not understand is acked anyway and reported as an `EdgeJournalGap` for its seq with the cause `rejected: <reason>`. One record from a newer satellite therefore never blocks a journal. Payload fields Ground Control does not know pass through, because schema versions only add fields.
   * **Rejected batches.** After three answers in a row that reject the same batch, the satellite drops it, and Ground Control records a gap.
   * **Otherwise one transaction runs:**
     * `OpenJournalCursor` upserts `satellite_journal_cursor(satellite_id, epoch, acked_seq)` with `epoch = GREATEST(stored, upload)`; a newer epoch resets `acked_seq` to 0. The upsert always returns the cursor and always locks its row, so concurrent uploads of one satellite run in turn, even the very first ones. It also reports whether it created or reset the cursor (`fresh`), judged against the epoch it read under the lock.
     * An upload from an older epoch is answered `{cursor epoch, 0}` and emits nothing.
     * Within the epoch the base is the greater of the cursor and `after_seq`. A base above the cursor covers seqs Ground Control will never have events for. It is emitted as one `EdgeJournalGap` with cause `new_epoch` for a fresh cursor, or `cursor_behind` otherwise: a range the satellite gave up, a restored Ground Control database, or a re-registered satellite. No upload can therefore wedge on a cursor that is behind the satellite.
     * The ack is the last uploaded seq. Records above the base become log-only edge events; records at or below the cursor are duplicates and emit nothing.
     * `AdvanceJournalCursor`, one data-modifying statement, moves the cursor and, only when it moved, appends the events to `event_outbox` already done. `JournalRepo.Advance` takes edge events only, so it never becomes a second way into the outbox.
   * **Failures.** A database failure after the heartbeat was recorded answers 200 without the `journal` object, so the satellite falls back to sending one record per beat.
   * **Budget.** Ground Control stores no journal table: the outbox is the only store of accepted records and the cursor alone deduplicates them. A journaled heartbeat costs at most four statements whatever the record count (BEGIN, `OpenJournalCursor`, `AdvanceJournalCursor`, COMMIT), five with the session renewal of plan item 4. A heartbeat without records keeps plan item 4's one-statement budget.
6. **Edge events (9.4).** `fleet.NewEdgeEvent` maps a record to its kind:
   * `replication` becomes `EdgeReplicated` or `EdgeDeleted` by its action, or `EdgeArtifactFailed` when it carries an error, for a failed replication or deletion;
   * `cri` becomes `EdgeCRIConfigured`;
   * `pull_through` becomes `EdgePullThrough`;
   * `dropped` becomes an `EdgeJournalGap` at its seq with cause `dropped` and the count;
   * Ground Control adds `EdgeJournalGap` events for a raised base and for records it cannot map.

   An edge event's payload is the record's payload with `name`, `epoch`, `seq` and `recorded_at` added at the same level. Edge events are log-only: nothing in Ground Control handles them, and webhooks (ADR-0019) read them from the outbox, whose retention replaces any journal retention. The emission must therefore not ship before the outbox retention of plan item 9.1 (branch 20); without it every journaled heartbeat grows the outbox for good.
7. **Report fields (6.6).** The report sets `latest_state_digest`, `latest_config_digest`, `last_sync_duration_ms`, `last_replication_error` and the new `applied_states [{ref, digest}]` from the last finished replication run.
   * The satellite state lists group states by tag, so only the per-reference group digests show whether a group update was applied; plan item 4's convergence compares them with the published artifact digests.
   * `applied_states` lists every group currently applied. A group whose replication failed keeps the digest it applied before, and an unchanged group keeps its digest.
8. **Cold path (6.1 to 6.3).**
   * An unchanged satellite or group state costs one HEAD per tick, like the config.
   * `replication_concurrency` bounds the groups replicating at once; 0 keeps one goroutine per group.
   * Replication gets its own Harbor store through `SetReplicationSource`. It is built with `NewRegistryStoreWithOptions` over the same options, so credential refreshes still apply, with `RegistryOptions.BandwidthBytesPerSec` set from `replication_bandwidth_bytes_per_sec`.
   * The store builds its client as retry(throttle(TLS transport)) with `lane.ThrottledTransport`. The TLS settings stay, retries spend the budget too, and the options stay a comparable value. The store the proxy uses is never throttled.
   * The registry client wraps its TLS transport in the retry transport instead of replacing it.
9. **Lifecycle (6.7).** Event schedulers run on the satellite's run context, and `Satellite.Stop` calls `EventScheduler.Stop`. That cancels a per-registration context, because `Scheduler.Stop` alone does not end a run loop. `SendEvent` reads the registrations under the lock and delivers with `Scheduler.TryTrigger`, which never blocks.

### Relation to ADR-0011 (open PR #597)

Both paths can exist, for different consumers:

* **The journal is the fleet path.** It is durable and at least once, and it ends at Ground Control's outbox and webhooks. A consumer that wants the whole fleet subscribes once, at Ground Control.
* **ADR-0011 stays the optional local path**, best effort as that ADR states, for consumers on the satellite's own network that should not depend on the round trip through Ground Control.
* **One vocabulary.** Both are fed from the same observation points of the replication cycle, and the journal's payloads use ADR-0011's field names, so a consumer sees the same data whichever path delivered it. CloudEvents types follow ADR-0011 where it defines one; the other edge kinds use Ground Control's own naming (ADR-0019, `io.harborsatellite.groundcontrol.<Kind>`). If ADR-0011 is not accepted, the two ADR-0011 names stay as they are.

| Edge kind | CloudEvents type |
|---|---|
| `EdgeReplicated` | `io.harborsatellite.artifact.synchronized` (ADR-0011) |
| `EdgeDeleted` | `io.harborsatellite.artifact.deleted` (ADR-0011) |
| `EdgeArtifactFailed`, `EdgePullThrough`, `EdgeCRIConfigured`, `EdgeJournalGap` | `io.harborsatellite.groundcontrol.<Kind>` |

## Consequences

* Good: edge events survive restarts, crashes and offline periods, and Ground Control emits each `(epoch, seq)` once.
* Good: no upload wedges. A cursor behind the satellite takes the satellite's floor, a satellite behind Ground Control's epoch continues above it, a record Ground Control cannot map is acked, and a batch Ground Control keeps rejecting is dropped after three tries. Each of these shows as a gap with its cause.
* Good: an unchanged state costs one HEAD per tick instead of a pull, replication can no longer saturate the uplink or take it from the proxy, and recording a pull never waits for a disk sync.
* Bad: a second on-disk structure at the edge with its own crash-recovery rules (`TestJournal_SurvivesKill9`, `TestJournal_TornTailIsTruncated`).
* Bad: heartbeats grow by up to `upload_batch` records while a backlog drains, and such a heartbeat costs up to four statements in Ground Control instead of one.
* Neutral: records dropped at the cap, under backpressure or after repeated rejection are lost, but visible as `EdgeJournalGap`.

## Implementation Status

| Part | Status |
|---|---|
| ADR | proposed |
| `internal/shared/edgejournal` | done (vocabulary) |
| `internal/satellite/journal` (6.4) | skeleton, pending tests |
| Migration `018_satellite_journal_cursor.sql`, queries `OpenJournalCursor`, `AdvanceJournalCursor` (6.5) | done, `TestSatelliteJournal_SQLContract`, `TestOpenJournalCursor_ConcurrentCreateReturnsTheRow`, `TestOpenJournalCursor_FreshSeesTheLatestCommittedEpoch` |
| Sync contract `journal` (request and response), `applied_states`, `last_replication_error`, 413 (6.5, 6.6) | in the spec, neither read nor set; `SYNC_MAX_BODY_BYTES` parsed only |
| `fleet.JournalService`, `fleet.JournalRepo` and its postgres adapter (6.5), `fleet.NewEdgeEvent` and the edge kinds (9.4) | skeleton, pending tests; kinds declared log-only |
| `state.StateArtifactChanged` (6.1), `RegistryOptions.BandwidthBytesPerSec`, `SetReplicationSource`, `lane.ThrottledTransport` and the `replication_*` keys (6.2), `state.RunSummary` (6.6), `Scheduler.TryTrigger` and `EventScheduler.Stop` (6.7) | skeleton, pending tests |
| Retry transport with TLS (6.3) | pending test |

## Validation

* The pending tests of plan items 6 and 9.4 in `internal/satellite/journal`, `lane`, `state`, `store`, `scheduler`, `events`, `internal/satellite`, `internal/groundcontrol/fleet`, `internal/groundcontrol/adapters/postgres` and `internal/groundcontrol/server` pass without their markers.
* The SQL contract tests stay green, and `TestRepository_JournalIngestTakesFourStatementsAndEmitsOnce` holds the four-statement budget.
* `task e2e-crash-recovery` stays green with the journal enabled.
