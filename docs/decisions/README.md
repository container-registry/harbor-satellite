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
| [ADR-0006](0006-satellite-lifecycle-states.md) | Satellite Lifecycle States | proposed; ADR-0016 designs its implementation |
| [ADR-0007](0007-security-plugins-parsec.md) | PARSEC Hardware-Backed Identity for Edge Satellites | deprecated (code removed in #526) |
| [ADR-0008](0008-parsec-integration-and-zero-trust-bootstrapping-flow.md) | PARSEC Integration & Zero-Trust Bootstrapping Flow | deprecated (code removed in #526) |
| [ADR-0009](0009-transparent-oci-registry-proxy.md) | Policy-Enforcing Transparent OCI Registry Proxy with ORAS | proposed, partly implemented (ORAS store landed in #648) |
| [ADR-0010](0010-peer-to-peer-distribution.md) | Copy OCI artifacts between trusted Satellites on an isolated network | proposed |
| [ADR-0013](0013-edge-store-concurrency-and-pull-through-streaming.md) | Edge store concurrency and pull-through streaming | proposed; skeleton only |
| [ADR-0014](0014-ground-control-async-plane.md) | Ground Control async plane: transactional outbox and level-triggered publisher | proposed; skeleton only |
| [ADR-0015](0015-edge-journal-and-upload-protocol.md) | Edge journal and upload protocol | proposed; skeleton only |
| [ADR-0016](0016-satellite-session-authentication.md) | Satellite sessions and the fleet hot path | proposed; skeleton only |
| [ADR-0017](0017-policy-engine-casbin.md) | Policy engine: Casbin for Ground Control and edge admission | proposed; skeleton only |
| [ADR-0018](0018-operator-identity-oidc.md) | Operator identity through OIDC and satellite credential rotation | proposed; skeleton only |

## Design Notes

- [Migrate Ground Control Into the Harbor Satellite Module](groundcontrol-internal-package-migration.md) (implemented in #497, #498, #614)

## Related Proposals

- [PARSEC Integration Proposal for Harbor Core](../proposals/parsec-harbor-core-integration.md): a proposal for Harbor itself, not a Harbor Satellite decision.
