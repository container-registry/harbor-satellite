---
title: "Documentation"
weight: 1
description: "Harbor Satellite 1.0 documentation: installation, quickstarts, architecture, security model, configuration and API reference, operations and design records."
source: "docs/README.md"
aliases:
  - "/docs/overview/"
---

## Get Started

- [Installation](/docs/get-started/installation/): release artifacts, Ground Control with Helm, containers or binaries, satellites with containers or systemd
- [Token quickstart](/docs/get-started/quickstart/token/): Ground Control and a satellite on one machine
- [SPIFFE/SPIRE quickstarts](/docs/get-started/quickstart/spiffe/): join token, X.509 PoP and SSH PoP attestation
- [SPIFFE end-to-end walkthrough](/docs/get-started/quickstart/end-to-end/): Harbor, SPIRE with X.509 PoP, Ground Control and a satellite, step by step
- [k3s reference architecture](/docs/guides/k3s-reference-architecture/)

## Concepts

- [Architecture](/docs/concepts/architecture/): desired state, the outbox and async plane, hot and cold paths, lifecycle, journal
- [Security model](/docs/concepts/security/): trust boundaries, secrets, identity, authorization, hardening
- [Security policy](/docs/concepts/security-policy/): reporting vulnerabilities, supported versions, disclosure

## Reference

- [Satellite configuration](/docs/reference/satellite/): flags, environment variables, `config.json`
- [Ground Control configuration](/docs/reference/ground-control/): environment variables, processes, CLI
- [Ground Control API](/docs/reference/api/): every route and its authentication; the contract is [`spec/groundcontrol/openapi.yaml`](https://github.com/container-registry/harbor-satellite/blob/main/spec/groundcontrol/openapi.yaml)
- [Event catalog](https://github.com/container-registry/harbor-satellite/tree/main/spec/events/v1): JSON Schema per webhook event kind

## Operations

- [Upgrading](/docs/operations/upgrading/), including the 1.0 upgrade notes
- [Database](/docs/operations/database/): PostgreSQL versions, TLS, pools and roles, retention
- [Backup and restore](/docs/operations/backup-and-restore/)
- [TLS](/docs/operations/tls/)
- [Monitoring](/docs/operations/monitoring/): satellite `/healthz` and `/metrics`, Ground Control `/health`
- [Troubleshooting](/docs/operations/troubleshooting/)

## Guides

- [Ground Control](/docs/guides/groundcontrol/)
- [Audit logging](/docs/guides/audit-logging/)
- [Keyless pulls and robot rotation](/docs/guides/keyless-pulls/)
- [Ground Control as a Harbor 3 extension](/docs/guides/harbor3-extension/)
- [Ground Control command migration](/docs/guides/groundcontrol-command-migration/)

## Design

- [Architecture decision records](/docs/decisions/)
- [Changelog](/docs/changelog/)
