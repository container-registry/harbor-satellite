# Monitoring

## Satellite: `/healthz` and `/metrics`

Each satellite serves both on `TELEMETRY_ADDR` (default `127.0.0.1:8586`), separate from the OCI proxy. Set `TELEMETRY_ADDR=0.0.0.0:8586` for Kubernetes probes or a scraper outside the host, and `TELEMETRY_ADDR=` to turn it off.

`GET /healthz` runs every check with a 2 second timeout and answers `200` when all pass, otherwise `503`:

```json
{"status":"ok","checks":{"proxy":"ok","store":"ok"}}
```

| Check | Fails when |
|---|---|
| `store` | The local OCI layout cannot be read. Not registered with a BYO registry. |
| `proxy` | The OCI proxy port does not accept a TCP connection. |

`GET /metrics` is Prometheus text format:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `satellite_proxy_pull_duration_seconds` | histogram | `resource` (`manifest`, `blob`), `outcome` (`ok`, `not_found`, `canceled`, `error`) | Proxy pull requests including the body transfer. |
| `satellite_proxy_pull_through_fills_total` | counter | `outcome` (`ok`, `error`) | `proxy` mode fills of a local miss from Harbor. |
| `satellite_replication_bytes_total` | counter | `outcome` (`ok`, `error`) | Bytes replicated into the local OCI layout. |
| `satellite_replication_duration_seconds` | histogram | `outcome` (`ok`, `error`) | One artifact replication into the local OCI layout. |
| `satellite_store_lock_wait_seconds` | histogram | `operation` (`publish`, `untag`, `collect`) | Wait for the exclusive index lock of the OCI layout. Readers hold it shared, so long waits mean writes queue behind pulls. |

The Go runtime (`go_*`) and process (`process_*`) collectors are included. The replication and lock metrics come from the OCI layout store; with a BYO registry they stay empty.

Suggested alerts:

- `/healthz` not `200` for 2 minutes.
- `rate(satellite_proxy_pull_duration_seconds_count{outcome="error"}[5m]) > 0`.
- `histogram_quantile(0.99, rate(satellite_store_lock_wait_seconds_bucket[5m])) > 1`: replication and garbage collection queue behind the index lock.
- `rate(satellite_proxy_pull_through_fills_total{outcome="error"}[10m]) > 0`: the satellite cannot reach Harbor for misses.

## Ground Control: `/metrics`

`groundcontrol serve` answers `GET /metrics` in Prometheus text format on `METRICS_ADDR` (default `:9464`), apart from the API port so the scrape port can stay inside the cluster. `METRICS_ADDR=` turns it off. A port in use stops startup. `groundcontrol worker` has no metrics endpoint; the outbox gauges come from the database, so any `serve` replica reports them.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `groundcontrol_http_requests_total` | counter | `route` (the route template, such as `/api/groups/{group}`, or `unmatched`), `method`, `code` | Requests answered, including `401`, `413` and `429` from the security middleware. |
| `groundcontrol_http_request_duration_seconds` | histogram | `route`, `method` | Request duration. |
| `groundcontrol_heartbeats_total` | counter | | Heartbeats this replica accepted into its buffer. |
| `groundcontrol_heartbeat_buffer_satellites` | gauge | | Satellites with heartbeats buffered on this replica and not yet written. |
| `groundcontrol_heartbeat_flushes_total` | counter | `outcome` (`ok`, `error`) | Writes of the heartbeat buffer, including the per-satellite retries after a failed batch. |
| `groundcontrol_heartbeat_flush_duration_seconds` | histogram | `outcome` | Duration of one buffer write. |
| `groundcontrol_outbox_events` | gauge | `state` (`pending`, `failed`) | Outbox events not done yet: `pending` have attempts left, `failed` ran out of `OUTBOX_MAX_ATTEMPTS`. |
| `groundcontrol_outbox_oldest_pending_age_seconds` | gauge | | Age of the oldest event not done; `0` without one. |
| `groundcontrol_outbox_stats_up` | gauge | | `1` when the outbox query of this scrape succeeded, `0` otherwise. |
| `go_sql_*` | gauge, counter | `db_name` (`api`, `fleet`, `async`) | `database/sql` pool statistics per plane: open, in use and idle connections, waits. A pool shared by several planes appears once, under the first name. |

The Go runtime (`go_*`) and process (`process_*`) collectors are included.

Suggested alerts:

- `groundcontrol_outbox_events{state="failed"} > 0`: Harbor changes that will not be retried without `POST /api/ops/outbox` or a new change.
- `groundcontrol_outbox_oldest_pending_age_seconds > 600`: no worker drains the outbox, or Harbor is unreachable.
- `rate(groundcontrol_heartbeat_flushes_total{outcome="error"}[5m]) > 0`: heartbeats are not written; `last_seen` stops moving.
- `sum(rate(groundcontrol_http_requests_total{code="429"}[5m])) by (route) > 0`.
- `go_sql_wait_count_total` growing: a pool is too small for its plane.

## Ground Control: `/health`, `/ping` and the Fleet API

| Endpoint | Use |
|---|---|
| `GET /ping` | Liveness probe. |
| `GET /health` | Readiness probe. `503` when the database does not answer, or when a background job of `serve` stopped (with `--worker=embedded` the async worker; also the registry probe, heartbeat flusher, rotation cache and policy watcher): the job appears in `components` as `{"status":"unhealthy","detail":"stopped"}` until the process restarts, and the log says `<job> stopped: <error>; /health reports unhealthy`. `200` with `"status":"degraded"` and `components.harbor` when Harbor has been unreachable for three `HARBOR_PROBE_INTERVAL`s. |
| `GET /api/satellites/stale` | Satellites without a heartbeat for three heartbeat intervals. Needs a running worker. |
| `GET /api/satellites/{satellite}/lifecycle` | Whether a satellite applied what Ground Control published (`converged`), and its last replication error. |
| `GET /api/ops/outbox?state=failed` | Harbor changes that did not go through. |

Example of a degraded answer:

```json
{"status":"degraded","components":{"harbor":{"status":"degraded","detail":"no recent contact","last_contact":"2026-11-12T09:14:03Z"}}}
```

Lifecycle changes can also be pushed to a receiver with [webhooks](../reference/api.md#webhooks), for example `SatelliteStale` and `SatelliteRevoked` events.

## Logs

The satellite logs JSON lines (zerolog) by default; `--json-logging=false` prints console output. Ground Control logs plain text to standard output and standard error. Neither logs secrets or response bodies, so log shipping is safe. Security events go to the audit log, see [Audit Logging](../guides/audit-logging.md).

Useful messages:

| Component | Message | Meaning |
|---|---|---|
| Satellite | `degraded replication from last-known state` | Harbor's state list could not be fetched; peers are used. |
| Satellite | `unreadable OCI index moved to ...` | The index was torn; every group replicates again. |
| Satellite | `admission deny` | A request matched no allow or a deny of the admission bundle. |
| Ground Control | `Harbor is not reachable yet (...); starting anyway` | Startup without Harbor. Writes are queued. |
| Ground Control | `outbox publisher: registry reachable again, re-armed N events` | Harbor is back after an outage. |
| Ground Control | `stale detector: N satellites stale` | Satellites stopped reporting. |
