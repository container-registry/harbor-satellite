# Troubleshooting

Start with the satellite's `/healthz` (`curl -s 127.0.0.1:8586/healthz` on the host), Ground Control's `/health`, and `GET /api/satellites/{satellite}/lifecycle`.

## Ground Control

| Symptom | Cause | Fix |
|---|---|---|
| Exits with `DB_SSLMODE "..." is not one of ...` | Invalid value. | Use `disable`, `allow`, `prefer`, `require`, `verify-ca` or `verify-full`. |
| Exits with `open database: ... SSL is not enabled on the server` | The default `DB_SSLMODE=require` against a server without TLS. | Enable TLS on PostgreSQL, or set `DB_SSLMODE=disable` on a trusted network. |
| Exits with `failed to run migrations: ... robot_accounts has several robots for satellites: ...` | Migration 023 found duplicate robot rows. | See [Upgrading](upgrading.md#migration-023-constraints-and-time-zones). |
| Exits with `bootstrap system admin` | `ADMIN_PASSWORD` unset or not matching the password policy on first start. | Set a password that passes the `PASSWORD_*` policy. |
| Exits with `POLICY_ENGINE "..."` or `OIDC_ROLE_MAP ...` | Invalid value. | Fix the variable named in the message. |
| Exits with `build fleet services: ...` and an OIDC error | `OIDC_ENABLED=true` and the issuer cannot be discovered. | Check `OIDC_ISSUER_URL` and network access to the provider. |
| Exits with `ROBOT_ROTATION_ENABLED: ... DATA_ENCRYPTION_KEY is not set` or the same with `WEBHOOKS_ENABLED` | Rotation or webhooks without a key. | Set `DATA_ENCRYPTION_KEY` to base64 of 32 bytes. |
| `/health` answers `503` with a component `"detail":"stopped"` | A background job of `serve`, such as the embedded async worker, returned. The log line `<job> stopped: <error>` names the cause. | Fix the cause and restart the process; a stopped job does not restart by itself. |
| `/health` answers `degraded` | No contact with Harbor for three `HARBOR_PROBE_INTERVAL`s. Writes are stored and queued. | Check `HARBOR_URL`, credentials and the network. The outbox drains once Harbor answers. |
| Group or config changes do not reach satellites | No worker is running, or events fail. | Run `serve` with the default `--worker=embedded` or a `groundcontrol worker`. Check `GET /api/ops/outbox?state=failed`. |
| `429` on ZTR | `RATE_LIMIT_ANONYMOUS` (10/1m per client IP). Behind a proxy without `AUDIT_TRUST_FORWARDED_HEADERS=true`, every client has the proxy address. | Trust the proxy headers, or raise `RATE_LIMIT_ANONYMOUS` for a mass rollout from one site. |
| `429` on heartbeats or refresh | `RATE_LIMIT_FLEET` (3/1s per satellite), or with `RATE_LIMIT_SPLIT=false` the shared limiter of 10 requests per minute per source address. | Set `RATE_LIMIT_SPLIT=true`; raise `RATE_LIMIT_FLEET` only for clients that send more than one heartbeat per second. |
| `429` before the password is checked | More than `AUTH_FAILURE_LIMIT` failed authentications from one IP. | Wait for `AUTH_FAILURE_WINDOW`, fix the client's credentials. |
| `401` for a correct password | The account is locked for `LOCKOUT_DURATION` after 5 failures, or `OIDC_DISABLE_PASSWORD_LOGIN=true`. | Wait, or log in through OIDC. |
| `409` on `PATCH /api/configs/{config}` | Another update changed the config at the same time. | Read the config again and repeat the patch. |
| `422` on config writes | The document fails satellite validation. | The message names the key. See [Satellite Configuration](../reference/satellite.md#app_config-keys). |
| Satellites never become stale | The stale detector runs in the worker. | Run a worker. |

## Satellite

| Symptom | Cause | Fix |
|---|---|---|
| Exits with `Missing required argument: ...` | A required setting is missing. | See [Required Settings](../reference/satellite.md#required-settings). |
| Exits with `listen for telemetry on 127.0.0.1:8586` | The port is in use, often by a second satellite on the host. | Set another `TELEMETRY_ADDR`, or `TELEMETRY_ADDR=` to disable it. |
| Exits with `listen for OCI proxy on 127.0.0.1:8585` | The proxy port is in use. | Set another `PROXY_PORT`. |
| Registration repeats with an `http://` refusal | The Ground Control URL is `http://` without `use_unsecure`. | Use HTTPS, or `USE_UNSECURE=true` for development. |
| Registration logs `503` and retries | Ground Control has not created the robot yet. | Normal for a few seconds. If it persists, no worker is running or Harbor is unreachable. |
| Registration answers `400 Invalid Token` | The token was used already, or never existed. | Create the satellite again or issue a new token. Tokens are single use. |
| Registration answers `401` | The token expired after 24 hours. | Issue a new token. |
| Heartbeat or registration answers `403` | The satellite is revoked. | `POST /api/satellites/{satellite}/unrevoke`. |
| Credential refresh answers `401` | The robot expired before it was refreshed. | Register the satellite again. Enable robot rotation to avoid it. |
| Pulls answer `MANIFEST_UNKNOWN` in `replica` mode | The artifact is not in any of the satellite's groups. | Add it to a group, or use `proxy` mode. |
| Pulls answer `DENIED` | Edge admission in `enforce` mode, or a bundle that does not load. | Check the `admission deny` log line and the bundle's `model_version`. |
| Pulls answer `UNSUPPORTED` | Push, delete, tag list, referrers and catalog are not served. | Push to Harbor. |
| Container runtime does not use the satellite | Mirror configuration not written, or Docker not restarted. | Run with root, check the `CRI ... configured` lines and `activity` in the satellite status. |
| `unreadable OCI index moved to ...` at startup | The index was torn, for example by power loss. | No action. Every group replicates again. |
| Disk keeps growing after groups shrank | Unreferenced content is kept for `STORE_GC_GRACE` (10 minutes) and while a pull reads it. | Wait for the next collection. |

## Collecting Information for a Support Request

- Version: the release tag of both binaries and images.
- Ground Control: `/health`, the `GET /api/ops/outbox?state=failed` output, and the log around the problem.
- Satellite: `/healthz`, `/metrics`, the JSON log with `log_level` `debug`, and `GET /api/satellites/{satellite}/lifecycle` and `/status`.

Logs and these endpoints contain no secrets. Do not attach `config.json` or a database dump.
