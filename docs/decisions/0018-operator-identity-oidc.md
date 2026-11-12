---
status: proposed
date: 2026-10-16
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Operator identity through OIDC and satellite credential rotation

## Context

Plan item 8 (identity) found these limits at `508b0d6`:

* **Operators are local users only.** ADR-0004 stores users and Argon2id password hashes in Ground Control; there is no single sign-on and no way to map a provider group to a role. Harbor 3 uses Dex.
* **The robot secret does two jobs.** It is the satellite's pull credential for Harbor and, until plan item 4, its Ground Control credential. It is refreshed only through D2's in-request rotation, so a satellite that misses the refresh loses both.
* **There is no keyless pull.** The satellite always pulls with a long-lived robot secret.

## Decision Drivers

* An OIDC login can never take over a local account, and a user without a role mapping gets no account.
* A login cannot be completed in another browser (state, nonce, PKCE and a browser binding).
* A break-glass path stays: the bootstrap `admin`.
* A satellite never loses its pull credential during a rotation, and the rotation costs the heartbeat no statement.

## Decision

1. **OIDC login (8.1).** Authorization code flow with PKCE (S256), a nonce and a browser binding cookie (`gc_oidc_binding`, HttpOnly, Secure, SameSite=Lax); only the binding's SHA-256 is stored in `oidc_login_states` (migration 021), which a callback takes once. The device flow (RFC 8628) serves the CLI (`auth login --oidc`). Claims map to a principal through `OIDC_ROLE_MAP`; no match is 403 and creates nothing, and the reserved `admin` is never mapped. A mapped user is created just in time with the unusable hash `!oidc` and linked in `user_identities`; a username held by a local account is 403. Every login re-derives the role from `OIDC_ROLE_MAP`, so a group removed at the provider demotes the user at the next login, or refuses it when no group maps. The `/oidc` routes are anonymous and sit behind the per-IP anonymous limiter with or without `RATE_LIMIT_SPLIT`; the device flow's poll interval is at least that limiter's spacing. Login states that are never taken are deleted after expiry by the worker's `LoginStateExpirer`. Ground Control sessions stay as today; `GetSessionPrincipalByToken` adds the identity's groups to the principal for scoped rules (ADR-0017).
2. **Password switch (8.2).** `OIDC_DISABLE_PASSWORD_LOGIN` refuses password login and Basic auth for everyone but the bootstrap `admin`.
3. **Keyless pull (8.3).** `internal/satellite/registryauth` exchanges a JWT-SVID for a pull token. Ports and fakes only, because Harbor 2.x has no token exchange.
4. **Robot rotation on Harbor 2.x (8.4).**
   * The worker's `RobotRotator` creates a successor robot (`SuccessorRobotName`, the satellite name cut so it fits `robot_accounts`) ahead of expiry with the current projects, only for satellites with a session or an SVID, because a robot-Basic beat never delivers it; a crash between create and stage is adopted on retry with a fresh secret.
   * The successor's secret is sealed with AES-GCM (`DATA_ENCRYPTION_KEY`) in `robot_rotations`.
   * Each serve process caches pending rotations (`RotationCache`); a heartbeat delivers one only to session or SVID beats whose generation is older, so delivery costs no statement. A robot-Basic satellite gets none, because it would switch its Basic credentials to a robot Ground Control does not accept yet.
   * The satellite acks the generation in its next heartbeat; the flush runs `AckRobotRotations` only for beats that carry one.
   * Promotion deletes the old robot first, then moves the successor into `robot_accounts` and drops the sealed secret; `RobotRotated` clears the published digest so the reconciler grants the successor its projects without republishing the fleet.
   * The tombstone deletes unpromoted successors too (`RobotRepo.HarborIDs`).
   * D2's refresh keeps rotating in the request until this ships, then retires.

## Consequences

* Good: single sign-on with role mapping; rotation without downtime; the robot secret is no longer the Ground Control credential.
* Bad: two robots per satellite exist during a rotation; a stored secret needs a data encryption key.
* Neutral: partly supersedes ADR-0004 (local users stay for the bootstrap admin and for installations without OIDC).

## Implementation Status

| Part | Status |
|---|---|
| ADR | proposed |
| Migration `021_operator_identities.sql`, identity and rotation queries | done, SQL contract tests |
| `authn` OIDC login and mapping, `adapters/oidc` (no go-oidc yet), postgres identity and rotation stores, `secrets.AESGCM`, `async.RobotRotator` and `RotationCache` | skeleton, pending tests |
| `/oidc/login`, `/oidc/callback`, `/oidc/device`, `/oidc/device/token`, sync `credentials` and `credentials_generation`, `app_config.registry_auth` | in the spec; the routes answer 501 |
| `registryauth`, `state.ApplyRotatedCredentials`, `auth login --oidc` | skeleton, pending tests |

## Validation

* The pending tests of plan item 8 pass without their markers, among them `TestSync_RotationOverlapWithoutDowntime` and `TestBudgetHeartbeat_RotationDeliveryAddsNoStatement`.
