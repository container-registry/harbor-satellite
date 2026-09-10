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
| [ADR-0004](0004-ground-control-authentication.md) | Ground Control Authentication | accepted |
| [PDR-0005](0005-spiffe-identity-and-security.md) | SPIFFE Identity and Security | accepted |
| [ADR-0006](0006-satellite-lifecycle-states.md) | Satellite Lifecycle States | proposed |
| [ADR-0007](0007-security-plugins-parsec.md) | PARSEC Hardware-Backed Identity for Edge Satellites | deprecated (code removed in #526) |
| [ADR-0008](0008-parsec-integration-and-zero-trust-bootstrapping-flow.md) | PARSEC Integration & Zero-Trust Bootstrapping Flow | deprecated (code removed in #526) |
| [ADR-0009](0009-transparent-oci-registry-proxy.md) | Policy-Enforcing Transparent OCI Registry Proxy with ORAS | proposed, partly implemented (ORAS store landed in #648) |
| [ADR-0010](0010-peer-to-peer-distribution.md) | Copy OCI artifacts between trusted Satellites on an isolated network | proposed |

## Design Notes

- [Migrate Ground Control Into the Harbor Satellite Module](ground-control-internal-package-migration.md) (implemented in #497, #498, #614)

## Related Proposals

- [PARSEC Integration Proposal for Harbor Core](../proposals/parsec-harbor-core-integration.md): a proposal for Harbor itself, not a Harbor Satellite decision.
