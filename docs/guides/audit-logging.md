# Audit Logging

Harbor Satellite emits a structured audit log of security-relevant events,
separate from the operational `zerolog` stream. The audit log is intended for
compliance (SOC2, ISO27001), incident investigation, and integration with
SIEM/security-monitoring tooling.

Both components - Satellite and Ground Control - produce their own audit log
when enabled.

## Format

Each event is one line of JSON. The event model is a canonical,
transport-neutral shape: the same fields map cleanly onto syslog (RFC 5424) and
OpenTelemetry, so a record never has to be reshaped or have fields renamed when
it is shipped to a SIEM.

```json
{
  "event_id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": "2026-05-01T10:00:00.123456Z",
  "severity": "warning",
  "component": "ground-control",
  "event_type": "session.login.failure",
  "operation": "login",
  "resource_type": "session",
  "outcome": "failure",
  "actor": "alice",
  "actor_type": "user",
  "source_ip": "10.0.0.5",
  "reason": "bad_password"
}
```

Every event carries eight **always-present** fields. The remaining nine are
**optional** and omitted when empty.

| Field           | Always? | Description |
| --------------- | ------- | ----------- |
| `event_id`      | yes | RFC 4122 UUID, generated per event for correlation. |
| `timestamp`     | yes | UTC, RFC 3339 with nanoseconds. |
| `severity`      | yes | One of `info` \| `warning` \| `error` \| `critical`. Derived from `outcome` (failures -> `warning`, otherwise `info`) unless set explicitly. Maps to syslog PRI (`critical`=2, `error`=3, `warning`=4, `info`=6) and OTel `SeverityText`/`SeverityNumber`. |
| `component`     | yes | Which side emitted the event: `satellite` or `ground-control`. Carried on the record so consumers don't infer origin from the file path. |
| `event_type`    | yes | Derived as `{resource_type}.{operation}.{outcome}`, e.g. `user.delete.success`. Provided so existing string-match rules keep working; the three parts are also available as their own fields. |
| `operation`     | yes | The verb: `login`, `create`, `delete`, `update`, `register`, `deregister`, `password_change`, `auth`, `revoke`, `unrevoke`. |
| `resource_type` | yes | The noun acted on: `user`, `satellite`, `config`, `session`, `policy`, `robot`. |
| `outcome`       | yes | `success` or `failure`. |
| `actor`         | no  | Username, satellite name, GC URL, or SPIFFE ID. Omitted when unknown (e.g., invalid token). |
| `actor_type`    | no  | Kind of principal: `user`, `robot`, `satellite`, `anonymous`, `system`. |
| `source_ip`     | no  | Client IP on Ground Control: the TCP `RemoteAddr` by default, or the first `X-Forwarded-For` hop only when `AUDIT_TRUST_FORWARDED_HEADERS=true`. Absent for outbound calls from the satellite. |
| `user_agent`    | no  | Client `User-Agent` on HTTP-originated events. |
| `request_id`    | no  | Correlation ID shared by every audit event from one Ground Control request. Reuses an inbound `X-Request-ID` header when present, otherwise Ground Control generates one. Absent on satellite-side and background events. |
| `satellite_id`  | no  | The satellite a satellite-scoped event relates to. |
| `resource`      | no  | The concrete target instance (e.g. the username created, the config name changed). |
| `reason`        | no  | Low-cardinality failure code (see catalogue). Maps to OTel `error.type`. Free-form failure text stays in `details`, never here. |
| `details`       | no  | Free-form, event-specific map. Omitted when empty. |

## Event catalogue

`event_type` is `{resource_type}.{operation}.{outcome}`. The table groups the
events emitted today.

| `event_type`                 | Source         | `reason` values | Emitted when |
| ---------------------------- | -------------- | --------------- | ------------ |
| `session.login.success`      | Ground Control | - | `/login` succeeds |
| `session.login.failure`      | Ground Control | `missing_credentials`, `account_locked`, `unknown_user`, `bad_password` | A login attempt is rejected |
| `user.create.success`        | Ground Control | - | `system_admin` creates a user |
| `user.delete.success`        | Ground Control | - | `system_admin` deletes a user |
| `user.password_change.success` | Ground Control | - | Self-service or admin-driven password change |
| `satellite.register.success` | Both           | - | Successful `/register`, `/ztr/{token}`, or SPIFFE ZTR; satellite logs its own successful registration |
| `satellite.register.failure` | Satellite      | `registration_failed`, `invalid_state_auth_config` | Satellite-side registration fails: network/HTTP error reaching Ground Control, or an invalid state-auth config is returned |
| `satellite.deregister.success` | Ground Control | - | `DELETE /satellites/{name}` |
| `satellite.auth.failure`     | Ground Control | `invalid_token`, `token_expired`, `missing_spiffe_identity`, `invalid_spiffe_id` | Invalid/expired token, or missing/invalid SPIFFE identity. Kept distinct from `satellite.register.failure` so brute-force alerts on auth failures are not triggered by benign network errors |
| `config.create.success`      | Ground Control | - | Config created via API |
| `config.update.success`      | Both           | - | GC: config updated via API. Satellite: config hot-reloaded |
| `config.delete.success`      | Ground Control | - | Config deleted via API |
| `satellite.revoke.success`   | Reserved       | - | Not yet emitted - see roadmap |
| `satellite.unrevoke.success` | Reserved       | - | Not yet emitted - see roadmap |
| `policy.pull_block.failure`  | Reserved       | - | Not yet emitted - depends on registry-level policy hooks |

### Config change payloads

Ground Control `config.*` events carry config detail in `details`:

- `config.create.success` -> `details.to`: the full new config.
- `config.delete.success` -> `details.from`: the full deleted config.
- `config.update.success` -> `details.changed`: a map of each changed field
  path (e.g. `state_config.auth.password`) to its `{ from, to }` values, so a
  change is auditable field-by-field. Unchanged fields are not listed, and a
  rotated secret still appears as a changed path even though its value stays
  redacted.

Secret values (passwords, tokens, credentials, keys) are replaced with
`[REDACTED]` before anything is written, so the audit log never contains config
secrets.

## Configuration

### Satellite

Add an `audit` block to the `app_config` section of the satellite config JSON.
Events are emitted as RFC 5424 syslog messages; the `syslog.target` selects the
destination.

```json
"audit": {
  "enabled": true,
  "syslog": {
    "target": "file",
    "tag": "harbor-audit",
    "socket_path": "/dev/log",
    "network": "udp",
    "address": "siem.example:514",
    "file": {
      "path": "/var/log/harbor-satellite/audit.log",
      "max_size_mb": 100,
      "max_backups": 7,
      "max_age_days": 30,
      "compress": true
    }
  }
}
```

| Field                  | Default        | Notes |
| ---------------------- | -------------- | ----- |
| `enabled`              | `false`        | Master switch. When false, all calls are no-ops. |
| `syslog.target`        | `file`         | `daemon`, `network`, or `file`. |
| `syslog.tag`           | `harbor-audit` | RFC 5424 APP-NAME. |
| `syslog.socket_path`   | `/dev/log`     | `daemon` target: local syslog socket. |
| `syslog.network`       | `udp`          | `network` target: `udp` or `tcp`. |
| `syslog.address`       | -              | `network` target: `host:port` of the SIEM. |
| `syslog.file.path`     | `./audit.log`  | `file` target: absolute path recommended in production. |
| `syslog.file.max_size_mb`  | `100`      | Rotate when the file exceeds this size. |
| `syslog.file.max_backups`  | `7`        | Keep this many rotated files. |
| `syslog.file.max_age_days` | `30`       | Drop rotated files older than this. |
| `syslog.file.compress`     | `true`     | gzip rotated files. |

Only the block for the chosen `target` is used; the others are ignored.
Rotation applies only to `target: file` - for `daemon` the OS rotates, and for
`network` there is no local file.

Omitting a rotation field uses its default (same as Ground Control's env-var
defaults). Setting `max_backups` or `max_age_days` to `0` is a deliberate
"retain everything" - no rotated files are pruned by count or age. Rotation is
provided by `gopkg.in/natefinch/lumberjack.v2`.

### Ground Control

Set environment variables in the GC `.env`:

```env
AUDIT_LOG_ENABLED=true
AUDIT_SYSLOG_TARGET=file
AUDIT_SYSLOG_TAG=harbor-audit
AUDIT_SYSLOG_SOCKET_PATH=/dev/log
AUDIT_SYSLOG_NETWORK=udp
AUDIT_SYSLOG_ADDRESS=siem.example:514
AUDIT_SYSLOG_FILE_PATH=/var/log/ground-control/audit.log
AUDIT_SYSLOG_FILE_MAX_SIZE_MB=100
AUDIT_SYSLOG_FILE_MAX_BACKUPS=7
AUDIT_SYSLOG_FILE_MAX_AGE_DAYS=30
AUDIT_SYSLOG_FILE_COMPRESS=true
AUDIT_TRUST_FORWARDED_HEADERS=false
```

`AUDIT_LOG_ENABLED=false` (default) disables the logger entirely.
`AUDIT_SYSLOG_TARGET` selects the destination (`daemon` | `network` | `file`);
only the variables for that target are read.

`AUDIT_TRUST_FORWARDED_HEADERS=false` (default) is the secure setting: the
audit `source_ip` is taken from the TCP `RemoteAddr` and cannot be forged by
clients. Set this to `true` only when GC sits behind a trusted reverse proxy
that you control; then the first entry of `X-Forwarded-For` (falling back to
`X-Real-IP`) is used.

For `target: file`, the rotation values must be non-negative
(`AUDIT_SYSLOG_FILE_MAX_SIZE_MB >= 1`, `AUDIT_SYSLOG_FILE_MAX_BACKUPS >= 0`,
`AUDIT_SYSLOG_FILE_MAX_AGE_DAYS >= 0`); for `target: network`,
`AUDIT_SYSLOG_ADDRESS` must be set. Invalid input causes GC to refuse to start
rather than silently drop events.

## Operational notes

- **Disabled-by-default.** The audit logger must be turned on explicitly. When
  off, all `Log(...)` calls are no-ops, so there is no performance impact.
- **No PII in `details`.** The instrumentation never logs passwords, tokens,
  or hashes. Tokens that appear in error paths (invalid ZTR tokens) are masked
  via the existing `maskToken` helper.
- **Forward to SIEM.** Use `target: network` to send RFC 5424 messages straight
  to a syslog/SIEM endpoint, `target: daemon` to hand off to the local syslog
  daemon, or `target: file` plus a log shipper (Filebeat, Vector, Fluent Bit).
  The message body is the canonical JSON, so the same fields are available
  whichever target is used.
- **Two destinations in production.** Satellite and Ground Control each emit
  their own stream. The `component` field tells them apart once aggregated;
  pivot on `event_type`, `severity`, `actor`, or `reason` in your SIEM.
- **OTel-ready.** The schema is designed so an OpenTelemetry exporter can be
  added without renaming fields - `severity` -> severity number, `component` ->
  `service.name`, and `operation` / `resource_type` / `outcome` / `reason`
  become first-class attributes instead of substrings parsed out of
  `event_type`.
- **Stable event types.** New event types will be added; the
  `{resource_type}.{operation}.{outcome}` identifiers are stable strings - safe
  to use in alerting rules.

## Roadmap

- `policy.pull_block.failure` - once registry-level policy hooks land, emit
  when the local Zot or fallback layer denies a pull.
- `satellite.revoke.success` / `satellite.unrevoke.success` - pending the
  revocation workflow added in Ground Control.
- **OpenTelemetry / CloudEvents transports** - the syslog transport ships
  today; OTel and CloudEvents exporters are tracked as follow-up work (the event
  model is already compatible - see Format). They plug into the same transport
  seam the syslog transport uses.
