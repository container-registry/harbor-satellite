---
status: proposed 
date: 2026-07-27
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Own the registry interface and use ORAS for transfer

## Context

Satellite currently uses Zot as its registry and Crane/go-containerregistry for
replication. The target proxy needs policy hooks before upstream access, before
publication, and before every registry mutation. It must also copy arbitrary OCI
descriptor graphs rather than only container images.

## Decision

Satellite will implement a focused OCI Distribution Specification HTTP interface
using Go's `net/http` stack and small routing helpers. It will use:

* `oras-go` to resolve and copy OCI artifacts;
* Satellite-owned policy, authentication, upload, and publication pipelines;
* the storage interface defined by the storage ADR; and
* upstream implementations as behavioral and conformance references.

The implementation will not fork Zot, Distribution, olareg, or
go-containerregistry. Reusable fixes should be contributed upstream.

## Why Own the Interface

The registry server is the point where Satellite must control:

* authentication and repository authorization;
* request and admission policy;
* upstream selection and credential boundaries;
* cache-miss coalescing;
* digest verification and quarantine;
* atomic publication;
* storage mode selection; and
* leases, eviction, reconciliation, and garbage collection.

Adapting a general-purpose registry would still require replacing or wrapping most
of these boundaries. A focused server keeps them explicit and avoids carrying
features that an edge proxy does not use.

Owning the interface does not itself provide OCI conformance. Conformance is a
release gate, not an assumption.

## Alternatives

### go-containerregistry `pkg/registry`

It is small and useful for tests and simple registries. It is not the selected
production base because its public surface does not provide the policy, quarantine,
recovery, storage, and GC boundaries Satellite needs.

Crane remains a useful command-line tool, but its image-oriented commands are not the
right core abstraction for copying arbitrary artifact graphs.

### Zot

Zot is OCI-conformant, embeddable, and feature rich. It includes sync, search,
scanning, signatures, metrics, multiple storage drivers, GC, and deduplication.

Satellite is removing it because:

* it owns a second configuration and lifecycle;
* policy must run inside Satellite's request and commit transaction;
* its feature set is larger than the target edge proxy needs; and
* storage behavior must expose Satellite's portable, hardlink, and shared modes.

Zot remains a useful reference, especially its application-level hardlink
deduplication and Bolt-backed cache.

### CNCF Distribution

Distribution is mature and highly compatible. It is a strong choice for a full
registry using filesystem or object-storage drivers.

It is not selected because its driver and repository-link model is not an OCI layout,
its dependency and runtime footprint are larger, and its policy hooks do not match
Satellite's admission transaction. It remains an important source for HTTP behavior,
upload semantics, and error handling.

### olareg

olareg is compact, OCI-conformant, and its directory store closely matches the
portable OCI-layout mode. It is the closest implementation reference.

Satellite still needs upstream routing, policy, cache coalescing, three storage modes,
and commit hooks as first-class interfaces. Before extensive implementation, the team
should verify whether new upstream extension points can meet those needs without a
fork. If they can, this decision should be revisited.

## Why ORAS

OCI artifacts are descriptor graphs. A valid root may describe an image, Helm chart,
SBOM, signature, Wasm module, model, or an unknown future type.

`oras-go` provides registry and OCI-layout targets and copies complete graphs without
requiring image-specific conversion. Satellite will use it for:

* desired-state replication;
* pull-through cache fill;
* import and export;
* copying referrers when required; and
* future peer transfer.

Satellite still owns authorization, policy, limits, verification, and the final local
commit. ORAS is a transfer library, not the inbound registry server.

## Required Protocol Surface

The first production slice includes:

* `GET /v2/`;
* manifest `GET` and `HEAD`;
* blob `GET` and `HEAD`, including ranges;
* OCI error responses and required headers; and
* upstream cache fill with request cancellation and coalescing.

Later slices add:

* blob upload start, patch, complete, and cancel;
* cross-repository mount;
* manifest push and delete;
* tag listing and referrers; and
* conditional requests and remaining conformance behavior.

Repository names, references, digests, upload IDs, and paths are parsed into typed
values. Raw URL fragments never become filesystem paths.

## Measured Direction

The prototype footprint was:

| Candidate | Binary | Idle RSS | Loaded RSS |
|---|---:|---:|---:|
| `pkg/registry` | 6.00 MiB | 5.55 MiB | 14.15 MiB |
| olareg | 6.66 MiB | 7.26 MiB | 17.81 MiB |
| Distribution | 23.05 MiB | 17.50 MiB | 140.55 MiB |
| Zot | 64.02 MiB | 49.66 MiB | 67.03 MiB |

The focused skeleton and `pkg/registry` were in the same performance class. These
numbers support the footprint goal but do not prove feature parity or correctness.
Distribution and Zot include production features the prototype did not implement.

## Boundaries

HTTP handlers depend on interfaces for policy, upstream resolution, transfer, and
storage. They do not know whether storage uses copies, hardlinks, or a shared blob
pool, and they do not access bbolt directly.

The registry must:

* verify digest and size before commit;
* never expose partial content;
* stream blobs without loading them completely into memory;
* preserve unknown valid media types;
* prevent credential forwarding across hosts by default; and
* return specification-compliant errors and headers.

## Consequences

* Good: Policy and publication order are explicit and testable.
* Good: ORAS provides one transfer model for arbitrary artifacts and future peers.
* Good: The focused runtime has a substantially smaller measured footprint than Zot
  or Distribution.
* Neutral: olareg and Distribution remain necessary behavior references.
* Neutral: ORAS replaces Crane for target-state transfers, but go-containerregistry
  may remain in unrelated code during migration.
* Bad: Replacing mature servers creates significant initial engineering and testing
  work.

## Validation

* Pass the official OCI Distribution conformance suite for every claimed category.
* Compare common responses with olareg and Distribution.
* Test Docker, Podman, containerd/nerdctl, ORAS, and Helm.
* Fuzz parsing, ranges, uploads, manifests, and cancellation.
* Test traversal, digest confusion, oversized graphs, concurrent completion, and
  crash recovery.

## References

* [go-containerregistry `pkg/registry`](https://github.com/google/go-containerregistry/tree/main/pkg/registry)
* [Crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane)
* [Zot](https://github.com/project-zot/zot)
* [CNCF Distribution](https://github.com/distribution/distribution)
* [olareg](https://github.com/olareg/olareg)
* [ORAS Go](https://github.com/oras-project/oras-go)
* [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md)
