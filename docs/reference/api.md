# Ground Control API Reference

The contract is the OpenAPI 3 document [`spec/groundcontrol/openapi.yaml`](../../spec/groundcontrol/openapi.yaml). Ground Control's router and the Go client in `pkg/groundcontrol` are generated from it, so a route that is not in the spec is not served. This page lists every route with the authentication it needs. Request and response schemas are in the spec.

Errors are JSON: `{"code": <status>, "message": "<text>"}`. Every response carries an `X-Request-ID` header.

## Authentication

| Class | Credentials |
|---|---|
| Public | None. |
| Anonymous | None, limited per client IP and with a 64 KiB request body limit. |
| Satellite | A satellite session (`Authorization: Bearer hss_...`), an X.509 SVID over mTLS, or robot Basic auth while `SATELLITE_ROBOT_AUTH=true`. A valid SVID wins over other credentials. |
| Operator | `Authorization: Bearer <session token>` from `/login` or OIDC, or `Authorization: Basic` with a username and password. |
| System admin | Operator with the `system_admin` role. |

With `POLICY_ENGINE=casbin` the Casbin policy decides every `/api` request instead of the operator and system admin classes below. Its baseline grants `system_admin` everything and `admin` satellites, groups except delete, configs, reading users, changing its own password and logging out.

Failed operator logins count toward the account lockout (5 failures, `LOCKOUT_DURATION`) whether they come through `/login` or Basic auth. More than `AUTH_FAILURE_LIMIT` failures from one IP within `AUTH_FAILURE_WINDOW` answer `429` before any password is checked.

## Health

| Method and path | Auth | Description |
|---|---|---|
| `GET /ping` | Public | Liveness. |
| `GET /health` | Public | `200` with `status` `healthy` or `degraded`, `503` `unhealthy` when the database does not answer. `components.harbor` reports `degraded` when the Harbor probe has had no contact for three `HARBOR_PROBE_INTERVAL`s. |

## Operator Sessions

| Method and path | Auth | Description |
|---|---|---|
| `POST /login` | Anonymous | `{"username", "password"}` to `{"token", "expires_at"}`. |
| `POST /api/logout` | Operator | Deletes the current session. |
| `GET /oidc/login` | Anonymous | Redirects to the OIDC provider and sets the `gc_oidc_binding` cookie. `404` when OIDC is off. |
| `GET /oidc/callback` | Anonymous | Completes the login in the browser that started it and returns `{"token", "expires_at"}`. |
| `POST /oidc/device` | Anonymous | Starts a device flow for the CLI. |
| `POST /oidc/device/token` | Anonymous | Polls the device flow. |

## Satellite Endpoints

| Method and path | Auth | Description |
|---|---|---|
| `POST /satellites/ztr` | Anonymous | Token registration. Returns robot credentials, the state URL and a satellite session. `400` for an unknown or used token, `401` for an expired one, `403` for a revoked satellite, `503` with `Retry-After` until the async worker created the robot. |
| `GET /satellites/spiffe-ztr` | Anonymous, SVID required | SPIFFE registration. The satellite name comes from the SVID. |
| `POST /satellites/sync` | Satellite | Heartbeat with status, applied state digests, inventory and journal. The answer can carry events (`refresh_credentials`), a renewed session, rotated robot credentials and `inventory_requested`. `403` for a revoked satellite. |
| `POST /satellites/refresh` | Satellite | Rotates the satellite's robot secret in Harbor and returns it. `401` when the robot has expired; the satellite then needs a new registration. |

## Satellites

| Method and path | Auth | Description |
|---|---|---|
| `GET /api/satellites` | Operator | Lists satellites. |
| `POST /api/satellites` | Operator | Registers a token-managed satellite `{"name", "groups", "config_name"}` and returns a one-time token (24 hours). Refused while SPIFFE or SPIRE is configured. |
| `POST /api/satellites/register` | System admin | Registers a SPIFFE satellite (join token, x509pop or sshpop attestation). |
| `GET /api/satellites/active` | Operator | Satellites in state `online`. |
| `GET /api/satellites/stale` | Operator | Satellites in state `stale`, with seconds since last seen. |
| `GET /api/satellites/{satellite}` | Operator | One satellite. |
| `DELETE /api/satellites/{satellite}` | Operator | Deletes the satellite. The worker removes its robot, state artifact and SPIRE entry. |
| `GET /api/satellites/{satellite}/status` | Operator | Latest status report. |
| `GET /api/satellites/{satellite}/images` | Operator | Images from the latest inventory report. |
| `GET /api/satellites/{satellite}/lifecycle` | Operator | Lifecycle state (`pending`, `online`, `stale`, `revoked`), applied and published digests and whether the satellite has converged. |
| `POST /api/satellites/{satellite}/revoke` | Operator | Revokes the satellite: its robot is disabled in Harbor, and sync and ZTR answer `403`. |
| `POST /api/satellites/{satellite}/unrevoke` | Operator | Returns it to `pending`; the next heartbeat makes it `online`. |

## Groups

| Method and path | Auth | Description |
|---|---|---|
| `GET /api/groups` | Operator | Lists groups. |
| `POST /api/groups/sync` | Operator | Creates or replaces a group `{"group", "artifacts": [{"repository", "tag", "type", "digest", "deleted"}]}`. The worker publishes the group state to Harbor. |
| `GET /api/groups/{group}` | Operator | One group. |
| `DELETE /api/groups/{group}` | System admin | Deletes a group. |
| `GET /api/groups/{group}/satellites` | Operator | Members. |
| `POST /api/groups/satellite` | Operator | Adds a satellite to a group. |
| `DELETE /api/groups/satellite` | Operator | Removes a satellite from a group. |

A satellite's robot can pull the projects of all its groups. Every membership change recomputes that set.

## Configs

| Method and path | Auth | Description |
|---|---|---|
| `GET /api/configs` | Operator | Lists configs. |
| `POST /api/configs` | Operator | Creates `{"config_name", "config"}`. `422` when the document fails satellite validation. |
| `GET /api/configs/{config}` | Operator | One config. |
| `PATCH /api/configs/{config}` | Operator | JSON Merge Patch. `409` when another update changed the config at the same time, `422` when the result fails validation. |
| `DELETE /api/configs/{config}` | Operator | Deletes a config no satellite uses. |
| `POST /api/configs/satellite` | Operator | Assigns a config to a satellite. |

## Users

| Method and path | Auth | Description |
|---|---|---|
| `GET /api/users` | Operator | Lists non-system users. |
| `POST /api/users` | System admin | Creates an `admin` user. |
| `GET /api/users/{username}` | Operator | One user. |
| `DELETE /api/users/{username}` | System admin | Deletes a user. |
| `PATCH /api/users/password` | Operator | Changes the caller's password. |
| `PATCH /api/users/{username}/password` | System admin | Resets a user's password. |

## Policy

These routes need system admin. The stored rules take effect only with `POLICY_ENGINE=casbin`.

| Method and path | Description |
|---|---|
| `GET /api/policies` | Lists stored rules. |
| `POST /api/policies` | Stores `{"subject", "scope", "resource_type", "action", "effect"}`. Subject `user:<name>`, `role:<role>` or `oidc:<group>`; scope `*` or `satellite:<name>`, `group:<name>`, `config:<name>`; effect `allow` or `deny`. |
| `DELETE /api/policies/{id}` | Deletes a rule. |
| `GET /api/role-bindings` | Lists role bindings. |
| `POST /api/role-bindings` | Gives `{"principal": "user:<name>" or "oidc:<group>", "role": "role:<name>"}` an additional custom role. |
| `DELETE /api/role-bindings/{id}` | Deletes a binding. |

Resource types: `satellite`, `group`, `config`, `user`, `spire`, `session`, `ops`, `policy`, `webhook`. Actions: `list`, `read`, `create`, `update`, `delete`, `sync`, `add_member`, `remove_member`, `assign_config`, `register`, `register_spiffe`, `revoke`, `unrevoke`, `change_password`, `set_password`, `logout`.

## Webhooks

System admin. Available with `WEBHOOKS_ENABLED=true`.

| Method and path | Description |
|---|---|
| `GET /api/webhooks` | Lists subscriptions with their delivery state. |
| `POST /api/webhooks` | Subscribes `{"url", "secret", "event_types"}`. The URL must be `https`, or `http` on a loopback host; the secret has at least 32 characters. Delivery starts with events committed after the subscription. |
| `DELETE /api/webhooks/{id}` | Deletes a subscription. |

Each delivery is a `POST` of a CloudEvents 1.0 JSON document (`application/cloudevents+json`) with headers `X-Ground-Control-Timestamp` (Unix seconds), `X-Ground-Control-Signature-256` (`sha256=` and the hex HMAC-SHA256 of `<timestamp>.<body>` with the subscription secret) and `Idempotency-Key` (the event ID). Delivery is at least once and in order per subscription. The event `type` is `io.harborsatellite.groundcontrol.<Kind>`. Payload schemas per kind are in [`spec/events/v1`](../../spec/events/v1).

Kinds emitted by Ground Control: `SatelliteCreated`, `SatelliteChanged`, `SatelliteDeleted`, `SatelliteStale`, `SatelliteRevoked`, `SatelliteUnrevoked`, `GroupChanged`, `GroupDeleted`, `ConfigChanged`, `ConfigDeleted`, `AssignmentChanged`, `PolicyChanged`, `RobotRotated`. The `Edge*` kinds (`EdgeReplicated`, `EdgeDeleted`, `EdgeArtifactFailed`, `EdgePullThrough`, `EdgeCRIConfigured`, `EdgeJournalGap`) come from satellite journal records and are log-only.

## Operations and SPIRE

| Method and path | Auth | Description |
|---|---|---|
| `GET /api/ops/outbox` | System admin | Outbox events not done yet. `state=failed` lists only those out of attempts. |
| `GET /api/spire/status` | System admin | SPIRE integration status. |
| `GET /api/spire/agents` | System admin | Attested SPIRE agents. |
