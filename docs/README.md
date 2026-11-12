# Harbor Satellite Documentation

## Get Started

- [Installation](installation.md): release artifacts, Ground Control with Helm, containers or binaries, satellites with containers or systemd
- [Token quickstart](../examples/deploy/no-spiffe/quickstart.md): Ground Control and a satellite on one machine
- [SPIFFE/SPIRE quickstarts](../examples/deploy/spiffe/README.md): join token, X.509 PoP and SSH PoP attestation
- [k3s reference architecture](guides/k3s-reference-architecture.md)

## Concepts

- [Architecture](architecture.md): desired state, the outbox and async plane, hot and cold paths, lifecycle, journal
- [Security model](security.md): trust boundaries, secrets, identity, authorization, hardening

## Reference

- [Satellite configuration](reference/satellite.md): flags, environment variables, `config.json`
- [Ground Control configuration](reference/ground-control.md): environment variables, processes, CLI
- [Ground Control API](reference/api.md): every route and its authentication; the contract is [`spec/groundcontrol/openapi.yaml`](../spec/groundcontrol/openapi.yaml)
- [Event catalog](../spec/events/v1): JSON Schema per webhook event kind

## Operations

- [Upgrading](operations/upgrading.md), including the 1.0 upgrade notes
- [Database](operations/database.md): PostgreSQL versions, TLS, pools and roles, retention
- [Backup and restore](operations/backup-and-restore.md)
- [TLS](operations/tls.md)
- [Monitoring](operations/monitoring.md): satellite `/healthz` and `/metrics`, Ground Control `/health`
- [Troubleshooting](operations/troubleshooting.md)

## Guides

- [Ground Control](guides/groundcontrol.md)
- [Audit logging](guides/audit-logging.md)
- [Keyless pulls and robot rotation](guides/keyless-pulls.md)
- [Ground Control as a Harbor 3 extension](guides/harbor3-extension.md)
- [Ground Control command migration](guides/groundcontrol-command-migration.md)

## Design

- [Architecture decision records](decisions/README.md)
- [Changelog](../CHANGELOG.md)
