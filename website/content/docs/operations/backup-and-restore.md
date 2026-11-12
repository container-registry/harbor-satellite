---
title: "Backup and Restore"
weight: 3
description: "The database dump contains operator session tokens and registration tokens in plain text. Store it encrypted."
source: "docs/operations/backup-and-restore.md"
---

## What to Back Up

| Item | Why | How |
|---|---|---|
| Ground Control database | Satellites, groups, configs, users, policies, webhooks, robot records, outbox, published digests | `pg_dump -Fc` or your platform's snapshots |
| `DATA_ENCRYPTION_KEY` | Unseals webhook secrets and staged robot rotations | Your secret store |
| Ground Control environment | Harbor credentials, OIDC client, TLS files | Your secret store or deployment repository |
| Harbor `satellite` project | Published group, config and satellite state artifacts | Part of your Harbor backup |
| Satellite config directory (optional) | `config.json` holds the robot credentials and session; `state.json`, `journal/` and `oci/` can be rebuilt | File backup, or none |

The database dump contains operator session tokens and registration tokens in plain text. Store it encrypted.

## Ground Control Database

Back up:

```bash
pg_dump -Fc -h "$DB_HOST" -U "$DB_USERNAME" -d "$DB_DATABASE" -f groundcontrol-$(date +%F).dump
```

Restore into an empty database with Ground Control stopped:

```bash
pg_restore --clean --if-exists --no-owner -h "$DB_HOST" -U "$DB_USERNAME" -d "$DB_DATABASE" groundcontrol-2026-11-12.dump
```

Start one Ground Control process. It applies the migrations newer than the dump.

After a restore:

- Operator sessions and satellite sessions issued after the dump are unknown. Operators log in again. Satellites with a session fall back to robot Basic auth while `SATELLITE_ROBOT_AUTH=true` and get a new session with their next heartbeat.
- Satellites registered after the dump are unknown and need to be registered again.
- Robots rotated after the dump: the database knows the older robot. If Harbor deleted it already, the satellite's next credential refresh fails with `401` and it needs a new registration. Keep the time between dump and restore short when rotation is on.
- Without the same `DATA_ENCRYPTION_KEY`, webhook subscriptions cannot sign deliveries. Delete and create them again. Staged rotations are discarded and staged again.

## Harbor State Artifacts

Ground Control pushes a state artifact only when its rendered digest differs from the digest recorded as published in `desired_state`. If the Harbor `satellite` project is lost while the database is kept, Ground Control does not notice by itself. To republish everything:

1. Clear the recorded publications:

   ```sql
   UPDATE desired_state SET published_digest = NULL, published_artifact_digest = NULL;
   ```

2. Queue a reconcile for every aggregate by changing it through the API, for example `POST /api/groups/sync` with each group's current artifacts and `PATCH /api/configs/{config}` with an empty patch `{}`. A group change also queues every member satellite.

3. Watch `GET /api/ops/outbox` until it is empty.

## Satellite

A satellite rebuilds everything except its credentials from Harbor. To replace a satellite's disk or device:

1. Delete the satellite (`DELETE /api/satellites/{satellite}`), or keep it and restore `config.json`.
2. Register it again with a new token or its SVID.
3. Start it with an empty config directory. It replicates its groups from Harbor, or from peers when peer distribution is on.

A `config.json` encrypted with `encrypt_config` can be read only on the device it was written on, because the key is derived from the device fingerprint.
