---
status: proposed
date: 2026-08-18
updated: 2026-08-30
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Air-gapped peer-to-peer OCI artifact distribution between trusted Satellites

## Context

Air-gapped deployments today bootstrap each Satellite independently from
offline OCI layouts or packages, even when a trusted peer Satellite on the
same isolated network already holds the needed artifact. This proposal
(tracked in issue #542) adds an opt-in mode for trusted Satellites to share
artifacts directly, without Internet, Harbor, or Ground Control connectivity.

This ADR builds on the ORAS-based store architecture landed in PR #648,
which replaced the embedded Zot registry with `OCIStore`
(`internal/satellite/store/oci.go`) and `RegistryStore`
(`internal/satellite/store/registry.go`) backed by `oras.land/oras-go/v2`.
Peer distribution attaches to this resolve/fetch/copy model — the peer is
an additional source registered through the same `Store` interface and
`NewOCIStore` / `NewRegistryStore` setup path.

## Decision Drivers

* Digest integrity must not be weaker than the existing replication path.
* Must function fully headless: no Ground Control, no Internet.
* Must not assume any particular peer registry implementation — Satellites
  can use the OCI image-layout store or a BYO external registry
  (see docker-compose.byo.yml), so eligibility checks must use only the
  plain OCI Distribution API, not registry-specific extensions.
* Opt-in and zero-cost when disabled; existing bootstrap/replication flows
  must be unaffected.
* Bounded resource use appropriate for constrained edge hardware.

## Considered Options

1. A custom peer transfer protocol.
2. Reusing registry-native sync/pull-through extensions.
3. Treating each peer as a plain OCI registry and using the standard
   Distribution API for both eligibility checks and transfer.

## Decision

Option 3. Every Satellite already fronts an OCI-compliant registry, so a
peer is addressed the same way any registry is: HEAD/GET against the
Distribution API. The only new surface this proposal adds is peer
selection (which peer to ask), health/reachability tracking, and
retry/backoff — not a new transfer protocol. This keeps the feature
portable across the OCI-layout store and BYO-registry peers by construction.

Peer discovery is explicit configuration only in this phase — a static
list of trusted peer endpoints. No mDNS, gossip, or Ground-Control-driven
discovery.

Artifact acquisition from peers is digest-locked: a peer is only accepted
as a source if it resolves the requested reference to the exact digest
recorded in the desired-state config, not merely "whatever this peer's
tag currently is."

## Trust Model

Peers are limited to a statically configured allowlist. Content integrity
comes from digest verification, not from trusting the peer — a peer being
"trusted" governs whether it's asked and whether its responses are
retried, not whether its bytes are accepted unverified. Longer-term,
identity for peer connections should align with the SPIFFE-based identity
work in ADR-0005 rather than introduce a parallel credential scheme.

Threats considered: wrong content (defeated by digest verification),
truncated transfer (defeated by full-closure verification before
publishing), a peer lying about having a tag (peers are never asked about
tags, only resolved digests), and peer-side denial of service (bounded by
timeouts, retries, and concurrency limits).

## Non-Goals (this term)

* Dynamic peer discovery (mDNS, DHT, gossip).
* Untrusted peer federation or cross-site Internet discovery.
* Ground Control scheduling or fleet-wide dashboards.
* Changes to image signing.

## Digest-Domain Rule — Current Enforcement

The digest-preferred source resolution rule proposed above is now partially
enforced on main: `Artifact.sourceIdentifier()` (`store/store.go`) prefers
the digest over the tag for both store paths (`RegistryStore`, `OCIStore`).
PR #637 will close the remaining gap in `DirectDeliverer` once merged
(`state/direct_delivery.go`), applying the same semantics to the k3s
tarball delivery path. This turns the rule from proposal into description
for the existing replication paths; the peer path inherits it by
construction when it attaches to the `Store` interface.

## Peer Serving Surface

PR #649 introduces a proxy package (`internal/satellite/proxy`) as the
intended serving surface for peers. The attach-point between the peer
selection logic proposed here and that proxy remains an open question for
maintainer discussion.

## Consequences

Positive: closes real operational overhead in air-gapped edge expansion;
portable across registry backends; no new attack surface beyond the
existing digest-verification guarantees.

Negative: peer selection and health tracking are new code paths to
maintain. The serving surface (PR #649's proxy package) is in flight and
may require coordination once both land.
