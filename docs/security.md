# Security Model

This page describes what each component trusts, which secrets exist and where they are kept. Report vulnerabilities privately to the maintainers, not in public issues.

## Trust Boundaries

| Boundary | Protection |
|---|---|
| Operator to Ground Control | HTTPS with `TLS_CERT_FILE`/`TLS_KEY_FILE` (TLS 1.2 minimum), optional client certificates (`TLS_CA_FILE`). Password or OIDC sessions. Lockout after 5 failures per username, `429` after `AUTH_FAILURE_LIMIT` failures per client IP. |
| Satellite to Ground Control | HTTPS or SPIFFE mTLS. The satellite refuses an `http://` Ground Control URL unless `use_unsecure` is set. Registration needs a one-time token or an SVID; afterwards a session token or the SVID. Anonymous routes accept at most 64 KiB of body. |
| Satellite to Harbor | HTTPS with the satellite's robot account. Pulls are limited to the projects of the satellite's groups plus `satellite`. |
| Workloads to satellite | The OCI proxy and the telemetry endpoint listen on `127.0.0.1` by default. The proxy is pull only and rejects non-loopback clients. Edge admission can restrict which repositories and references are served. |
| Ground Control to Harbor | `HARBOR_USERNAME`/`HARBOR_PASSWORD`, an account that can create projects and robot accounts. |

## Secrets

| Secret | Where | At rest |
|---|---|---|
| Harbor admin credentials | Ground Control environment | Plain, protect the environment or secret store. |
| `DATA_ENCRYPTION_KEY` | Ground Control environment | Plain. Seals staged robot secrets and webhook secrets in the database with AES-256-GCM. |
| Operator passwords | `users` table | Argon2id hash. |
| Operator session tokens | `sessions` table | Plain token. Treat database backups as secret. |
| Registration tokens | `satellite_token` table | Plain, single use, 24 hours. |
| Satellite session tokens | `satellite_sessions` table | SHA-256 hash. |
| Robot secrets | `robot_accounts` table | Argon2id hash, used to verify robot Basic auth. |
| Robot secret on the satellite | `config.json` | Plain unless `app_config.encrypt_config` is on (AES-256-GCM, key derived from the device fingerprint with Argon2id, Linux builds only). |
| Webhook secrets | `webhook_subscriptions` | Sealed with `DATA_ENCRYPTION_KEY`. |

Ground Control and the satellite never log passwords, secrets, tokens or response bodies of Harbor and Ground Control. A repository-wide test (`test/arch`) fails the build on a log argument named like one.

## Satellite Identity

- **Token registration.** An operator creates the satellite and receives a token. The first successful registration consumes it in one statement, so concurrent attempts with the same token cannot both succeed. A used token answers `400`, an expired one `401`.
- **SPIFFE registration.** The satellite presents an X.509 SVID from its SPIRE agent. Attestation methods: join token, X.509 proof of possession, SSH proof of possession. The embedded SPIRE server supports join tokens only.
- **Sessions.** Registration and heartbeats hand out a satellite session that is renewed in the last third of its lifetime (`SATELLITE_SESSION_TTL`, default 30 days).
- **Revocation.** `POST /api/satellites/{satellite}/revoke` disables the robot in Harbor and makes sync and registration answer `403`. Processes cache verified sessions for up to `SATELLITE_SESSION_CACHE_TTL` (default 30s), so a revocation reaches every process within that time.
- **Robot rotation.** With `ROBOT_ROTATION_ENABLED=true`, robots of satellites with a session or SVID are replaced before they expire; the old robot is deleted only after the satellite confirmed the new one. See [Keyless Pulls and Robot Rotation](guides/keyless-pulls.md).

## Operator Authorization

| Engine | Behavior |
|---|---|
| `builtin` (default) | `system_admin` may do everything. `admin` may do everything except user management, group deletion, SPIRE, SPIFFE registration, policies, role bindings, webhooks and the outbox. |
| `casbin` | Each `/api` request maps to a resource type and action and is checked against the baseline and the stored rules. Rules can be scoped to one satellite, group or config, can deny, and can name OIDC groups. |

With OIDC, `OIDC_ROLE_MAP` maps provider groups to `admin` or `system_admin`, and `OIDC_DISABLE_PASSWORD_LOGIN=true` leaves password login only to the bootstrap `admin`, as a break-glass account.

## Audit

Both components can write security events (logins, registrations, configuration changes, credential refreshes) to syslog or OpenTelemetry. See [Audit Logging](guides/audit-logging.md). Set `AUDIT_TRUST_FORWARDED_HEADERS=true` only behind a proxy that appends to `X-Forwarded-For`, otherwise clients choose their recorded source address; set `AUDIT_TRUSTED_PROXY_HOPS` to the number of such proxies.

## Supply Chain

Release container images are signed with Cosign (keyless). Release archives carry SBOMs and a `checksums.txt`.

## Hardening Checklist

- Serve Ground Control over HTTPS, and set `USE_UNSECURE=false` on every satellite.
- Use `DB_SSLMODE=verify-full` with `DB_SSLROOTCERT` for a database on another host.
- Set `RATE_LIMIT_SPLIT=true`.
- Rotate `ADMIN_PASSWORD` after the first start, and prefer OIDC for operators.
- Enable `ROBOT_ROTATION_ENABLED` with a `DATA_ENCRYPTION_KEY` kept in a secret store.
- Keep `TELEMETRY_ADDR` on loopback unless a scraper needs it, and restrict that port at the network.
- Enable audit logging and ship it off the host.
- Protect database backups: they contain operator session tokens and registration tokens.
