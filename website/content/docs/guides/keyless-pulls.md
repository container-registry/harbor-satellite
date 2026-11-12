---
title: "Keyless Pulls and Robot Rotation"
weight: 4
description: "A satellite pulls from Harbor with credentials Ground Control hands it. Two mechanisms keep those credentials short-lived; which one applies depends on the Harbor version."
source: "docs/guides/keyless-pulls.md"
---

A satellite pulls from Harbor with credentials Ground Control hands it. Two
mechanisms keep those credentials short-lived; which one applies depends on
the Harbor version.

## Harbor 2.x: rotated robot accounts (available)

Ground Control rotates each satellite's robot account before it expires
(plan item 8.4):

1. The worker finds robots expiring within `ROBOT_ROTATION_BEFORE_EXPIRY`
   (default 72h) whose satellite holds a Ground Control session or a SPIFFE
   identity.
2. It creates a successor robot with the satellite's current projects,
   seals its secret with `DATA_ENCRYPTION_KEY` (AES-256-GCM) and stages it.
3. The next heartbeat of the satellite answers the successor in
   `credentials`. The satellite stores it with its generation in
   `config.json` and reports the generation with its next heartbeat.
4. After that acknowledgement the worker deletes the old robot and promotes
   the successor. Old and successor robot both work until then, so a
   rotation never interrupts pulls.

Robot Basic satellites do not get rotations: they would switch to a robot
Ground Control does not accept for Basic auth yet. They get a session on
their next heartbeat and rotations from then on.

Settings:

| Variable | Default | Meaning |
|---|---|---|
| `ROBOT_ROTATION_ENABLED` | `false` | Run the rotator (worker) and the delivery cache (serve) |
| `DATA_ENCRYPTION_KEY` | none | Base64 of 32 bytes; required when rotation is enabled |
| `ROBOT_ROTATION_BEFORE_EXPIRY` | `72h` | Rotate robots expiring within this window |
| `ROBOT_ROTATION_INTERVAL` | `10m` | How often the worker looks for robots to rotate |
| `ROBOT_ROTATION_CACHE_REFRESH` | `30s` | How often each serve process reloads pending rotations |

Generate a key with `openssl rand -base64 32`. Losing the key loses only
staged, unacknowledged successors; the rotator stages new ones.

## Harbor 3: keyless pulls (not available yet)

The target design (plan item 8.3, ADR-0018) removes stored robot secrets:
the satellite fetches a JWT-SVID from its SPIRE agent and exchanges it for a
short-lived Harbor pull token through Harbor 3 workload identity
federation.

What exists today:

- `internal/satellite/registryauth`: the `Provider` port the pull path is
  meant to use, `StaticProvider` for robot credentials and
  `WorkloadIdentityProvider`, which fetches a JWT-SVID, calls a
  `TokenExchanger` and caches the token until 30 seconds before it expires.
- `app_config.registry_auth` with `mode` (`robot` or `workload_identity`)
  and `audience`.

The gap: Harbor 3 has not released its token exchange API, so there is no
`TokenExchanger` implementation and nothing composes
`WorkloadIdentityProvider`. Until it exists, Ground Control and the
satellite reject `registry_auth.mode: workload_identity` with a validation
error instead of silently pulling with the robot. Satellites keep pulling
with rotated robot accounts (above).

When Harbor 3 ships the exchange, the remaining work is an exchanger
adapter for its API, a JWT-SVID source over the SPIRE Workload API, wiring
the provider into the satellite's registry client, and an end-to-end test
that pulls without stored credentials.
