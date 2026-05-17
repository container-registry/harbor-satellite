# Audit Logging

Harbor Satellite emits a structured audit log of security-relevant events,
separate from the operational `zerolog` stream. The audit log is intended for
compliance (SOC2, ISO27001), incident investigation, and integration with
SIEM/security-monitoring tooling.

Both components — Satellite and Ground Control — produce their own audit log
when enabled.

## Format

Each event is one line of JSON.

```json
{
  "event_id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": "2026-05-01T10:00:00.123456Z",
  "event_type": "user.login.failure",
  "actor": "alice",
  "source_ip": "10.0.0.5",
  "details": {
    "reason": "bad_password"
  }
}
```

| Field        | Description |
| ------------ | ----------- |
| `event_id`   | RFC 4122 UUID, generated per event for correlation. |
| `timestamp`  | UTC, RFC 3339 with nanoseconds. |
| `event_type` | Stable identifier from the catalogue below. |
| `actor`      | Username, satellite name, GC URL, or SPIFFE ID. Empty when unknown (e.g., invalid token). |
| `source_ip`  | Client IP (honors `X-Forwarded-For` first hop) on Ground Control; empty for outbound calls from the satellite. |
| `details`    | Free-form, event-specific. Omitted when empty. |

## Event catalogue

| `event_type`               | Source         | Emitted when |
| -------------------------- | -------------- | ------------ |
| `user.login.success`       | Ground Control | `/login` succeeds |
| `user.login.failure`       | Ground Control | Missing creds, locked account, unknown user, bad password |
| `user.created`             | Ground Control | `system_admin` creates an admin user |
| `user.deleted`             | Ground Control | `system_admin` deletes a user |
| `user.password_changed`    | Ground Control | Self-service or admin-driven password change |
| `satellite.registered`     | Both           | Successful `/register`, `/ztr/{token}`, or SPIFFE ZTR; satellite logs successful local registration |
| `satellite.deregistered`   | Ground Control | `DELETE /satellites/{name}` |
| `satellite.auth.failure`   | Both           | Invalid/expired token; missing or invalid SPIFFE identity; satellite-side registration failures |
| `satellite.revoked`        | Reserved       | Not yet emitted — see roadmap |
| `satellite.unrevoked`      | Reserved       | Not yet emitted — see roadmap |
| `policy.pull_blocked`      | Reserved       | Not yet emitted — depends on registry-level policy hooks |
| `config.changed`           | Both           | GC: config create/update/delete via API. Satellite: hot-reloaded config keys. |

## Configuration

### Satellite

Add an `audit` block to the `app_config` section of the satellite config JSON:

```json
"audit": {
  "enabled": true,
  "file_path": "/var/log/harbor-satellite/audit.log",
  "max_size_mb": 100,
  "max_backups": 7,
  "max_age_days": 30
}
```

| Field          | Default        | Notes |
| -------------- | -------------- | ----- |
| `enabled`      | `false`        | Master switch. When false, all calls are no-ops. |
| `file_path`    | `./audit.log`  | Absolute path recommended in production. |
| `max_size_mb`  | `100`          | Rotate when the file exceeds this size. |
| `max_backups`  | `7`            | Keep this many rotated files. |
| `max_age_days` | `30`           | Drop rotated files older than this. |

Rotation is provided by `gopkg.in/natefinch/lumberjack.v2`; old files are
gzip-compressed.

### Ground Control

Set environment variables in the GC `.env`:

```env
AUDIT_LOG_ENABLED=true
AUDIT_LOG_PATH=/var/log/ground-control/audit.log
AUDIT_LOG_MAX_SIZE_MB=100
AUDIT_LOG_MAX_BACKUPS=7
AUDIT_LOG_MAX_AGE_DAYS=30
AUDIT_TRUST_FORWARDED_HEADERS=false
```

`AUDIT_LOG_ENABLED=false` (default) disables the logger entirely.

`AUDIT_TRUST_FORWARDED_HEADERS=false` (default) is the secure setting: the
audit `source_ip` is taken from the TCP `RemoteAddr` and cannot be forged by
clients. Set this to `true` only when GC sits behind a trusted reverse proxy
that you control; then the first entry of `X-Forwarded-For` (falling back to
`X-Real-IP`) is used.

When `AUDIT_LOG_ENABLED=true`, the rotation values must be non-negative
(`MAX_SIZE_MB >= 1`, `MAX_BACKUPS >= 0`, `MAX_AGE_DAYS >= 0`). Invalid input
causes GC to refuse to start rather than silently drop events.

## Operational notes

- **Disabled-by-default.** The audit logger must be turned on explicitly. When
  off, all `Log(...)` calls are no-ops, so there is no performance impact.
- **No PII in `details`.** The instrumentation never logs passwords, tokens,
  or hashes. Tokens that appear in error paths (invalid ZTR tokens) are masked
  via the existing `maskToken` helper.
- **Forward to SIEM.** Point a log shipper (Filebeat, Vector, Fluent Bit) at
  the audit file. The JSON-per-line format requires no parsing rules.
- **Two files in production.** Satellite and Ground Control each write their
  own file. Aggregate them by `event_type`/`actor` in your SIEM.
- **Stable event types.** New event types will be added; existing identifiers
  are stable strings — safe to use in alerting rules.

## Roadmap

- `policy.pull_blocked` — once registry-level policy hooks land, emit when
  the local Zot or fallback layer denies a pull.
- `satellite.revoked` / `satellite.unrevoked` — pending the revocation
  workflow added in Ground Control.
