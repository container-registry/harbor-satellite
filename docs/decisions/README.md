# Decisions

This directory contains decision records for Harbor Satellite.

For new ADRs, use [_adr-template-short.md](_adr-template-short.md) or [_adr-template-long.md](_adr-template-long.md) as the basis.
More information on MADR is available at <https://adr.github.io/madr/>.
General information about architectural decision records is available at <https://adr.github.io/>.

## Index

| Record | Title | Status |
|---|---|---|
| [ADR-0001](0001-skopeo-vs-crane.md) | Skopeo vs Crane | accepted; ORAS now backs the local store (#648), Crane still used elsewhere |
| [ADR-0002](0002-zot-vs-docker-registries.md) | Zot vs Docker Registries | superseded (Zot replaced by ORAS in #648) |
| [ADR-0003](0003-remote-config-injection.md) | Remote Config Injection | accepted |
| [ADR-0004](0004-groundcontrol-authentication.md) | Ground Control Authentication | accepted; partly superseded by ADR-0016 and ADR-0018 |
| [PDR-0005](0005-spiffe-identity-and-security.md) | SPIFFE Identity and Security | accepted |
| [ADR-0006](0006-satellite-lifecycle-states.md) | Satellite Lifecycle States | accepted; implemented through ADR-0016 |
| [ADR-0007](0007-security-plugins-parsec.md) | PARSEC Hardware-Backed Identity for Edge Satellites | deprecated (code removed in #526) |
| [ADR-0008](0008-parsec-integration-and-zero-trust-bootstrapping-flow.md) | PARSEC Integration & Zero-Trust Bootstrapping Flow | deprecated (code removed in #526) |
| [ADR-0009](0009-transparent-oci-registry-proxy.md) | Policy-Enforcing Transparent OCI Registry Proxy with ORAS | proposed; ORAS store, proxy modes and request-level admission implemented, `cache` mode and content-aware policy not |
| [ADR-0010](0010-peer-to-peer-distribution.md) | Copy OCI artifacts between trusted Satellites on an isolated network | proposed; peer-first and degraded replication implemented |
| [ADR-0013](0013-edge-store-concurrency-and-pull-through-streaming.md) | Edge store concurrency and pull-through streaming | accepted; implemented |
| [ADR-0014](0014-ground-control-async-plane.md) | Ground Control async plane: transactional outbox and level-triggered publisher | accepted; implemented |
| [ADR-0015](0015-edge-journal-and-upload-protocol.md) | Edge journal and upload protocol | accepted; satellite side implemented, Ground Control ingest not |
| [ADR-0016](0016-satellite-session-authentication.md) | Satellite sessions and the fleet hot path | accepted; implemented |
| [ADR-0017](0017-policy-engine-casbin.md) | Policy engine: Casbin for Ground Control and edge admission | accepted; implemented |
| [ADR-0018](0018-operator-identity-oidc.md) | Operator identity through OIDC and satellite credential rotation | accepted; OIDC and robot rotation implemented, keyless pulls wait for Harbor 3 |
| [ADR-0019](0019-ground-control-hosting-and-extension-points.md) | Ground Control hosting and extension points | accepted; webhooks and event catalog implemented, registry event adapter and PostgreSQL 19 outbox not |
| [ADR-0020](0020-postgresql-19-minimum.md) | PostgreSQL 19 as the minimum version | accepted; not implemented (no version check, tests on 17) |

## Design Notes

- [Migrate Ground Control Into the Harbor Satellite Module](groundcontrol-internal-package-migration.md) (implemented in #497, #498, #614)

## Related Proposals

- [PARSEC Integration Proposal for Harbor Core](../proposals/parsec-harbor-core-integration.md): a proposal for Harbor itself, not a Harbor Satellite decision.
