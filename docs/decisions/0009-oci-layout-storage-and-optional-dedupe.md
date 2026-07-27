---
status: proposed 
date: 2026-07-27
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Use OCI storage with bbolt-assisted deduplication

## Context

Satellite needs a read-mostly local registry and pull-through cache. Content is
normally written once after policy and digest verification, then served many times.

The storage design must:

* preserve arbitrary OCI descriptor graphs;
* run without an external service;
* deduplicate content where useful;
* work on filesystems with and without hardlinks;
* recover after crashes;
* support low-resource edge hardware; and
* make portability tradeoffs explicit.

## Decision

Satellite will store verified OCI content on the filesystem and use embedded bbolt as
a persistent dedupe cache.

bbolt maps a digest to a versioned composite value such as:

```text
digest -> {
    path,
    size,
    usage_count,
    verified_at,
    format_version
}
```

Values use an explicitly versioned binary encoding. `usage_count` is an optimization,
not the garbage-collection authority.

The bbolt file is derived state:

* artifact bytes remain regular files;
* repository roots and descriptor graphs define reachability;
* policy and credentials are never stored in bbolt;
* a missing, incompatible, or corrupt cache can be deleted and rebuilt; and
* reads verify that a cached path still exists before using it.

This keeps the common lookup fast without making bbolt a future users/roles database.
Relational metadata, if needed later, will receive a separate decision.

## Filesystem Layout

```text
<root>/
|-- repositories/
|   `-- <upstream-id>/
|       `-- <repository>/
|           |-- oci-layout
|           |-- index.json
|           `-- blobs/
|               `-- <algorithm>/<digest>
|-- shared/
|   `-- blobs/<algorithm>/<digest>
|-- metadata/
|   `-- dedupe.db
|-- uploads/
`-- quarantine/
```

`shared/` is used only in shared mode. Upload and quarantine files are not visible to
registry clients.

## OCI Metadata

An OCI layout contains:

* `oci-layout`, the layout version marker;
* `index.json`, the root descriptor and tag list; and
* `blobs/<algorithm>/<digest>`, all descriptor-addressed content.

`index.json` is not the manifest returned by
`GET /v2/<name>/manifests/<reference>`. It resolves a reference to a descriptor. The
referenced manifest or index is stored as a digest-addressed blob.

The same blob namespace stores manifests, indexes, configurations, layers, Helm
charts, SBOMs, signatures, attestations, Wasm modules, models, and unknown future OCI
artifacts. Storage does not branch on artifact type.

## Storage Modes

### Portable

Every repository is a complete, independent OCI layout. When a digest is referenced
by several repositories, its bytes are copied into each layout.

* Works without hardlinks.
* Can be copied, inspected, validated, or exported directly.
* Does not depend on bbolt for reads.
* Uses the most disk space.

This is the compatibility fallback and the safest mode for removable media or an
unknown filesystem.

### Hardlink

Every repository remains a complete OCI layout, but duplicate blob entries are
hardlinks to an existing verified file.

* Provides physical deduplication while preserving complete layouts.
* Reads use the requested repository path directly.
* bbolt finds a source path without scanning all repositories.
* Requires the source and destination to be on the same hardlink-capable filesystem.

This is the preferred local-filesystem mode.

If hardlink creation fails, configuration decides whether startup or commit fails, or
whether Satellite explicitly falls back to portable copies. The fallback is never
silent.

### Shared

Each verified digest is stored once in the shared blob pool. Repository metadata
references the digest, and the storage layer resolves it to the shared file.

* Deduplicates without hardlinks.
* Avoids one blob directory entry per repository.
* Works well when repository layouts and the shared pool use different filesystems.
* Requires materialization before exporting a repository as a complete OCI layout.

Repository authorization and reachability still come from repository roots and
descriptor graphs, not from the existence of a digest in the shared pool. The bbolt
cache accelerates resolution; deterministic shared paths and reconciliation allow it
to be rebuilt.

## Commit Flow

1. Stream incoming bytes to quarantine while hashing.
2. Verify the expected digest and size.
3. Apply content policy.
4. Lock the digest in the process.
5. Check bbolt and validate any returned path.
6. Commit according to the configured mode:
   * copy into the repository for `portable`;
   * create a repository hardlink for `hardlink`;
   * atomically install once in the shared pool for `shared`.
7. Set immutable payload files to mode `0444` where supported.
8. Atomically update the repository root.
9. Commit the bbolt cache update.
10. Release the digest lock.

A cache-update failure does not invalidate already committed content. Reconciliation
repairs the cache.

## Publication and Recovery

Repository root updates use write, `fsync`, rename, and directory `fsync` where the
platform supports them. Readers see either the old complete root or the new complete
root.

At startup Satellite:

* removes or resumes incomplete uploads according to policy;
* validates repository roots;
* checks the bbolt format version;
* rebuilds bbolt when it is missing, corrupt, or incompatible;
* removes stale digest-to-path entries; and
* verifies shared-pool and hardlink sources before reuse.

bbolt is mmap-backed and its file is endian-specific. On a different endian
architecture, or when a 32-bit target cannot map the file safely, Satellite rebuilds
the derived cache rather than migrating it. Cache size must be bounded on 32-bit
targets.

## Deletion and Garbage Collection

Garbage collection follows descriptor reachability, active uploads, leases, pins, and
retention policy. It does not trust bbolt `usage_count` or filesystem link count as
the sole source of truth.

* In portable mode, unreachable repository copies are deleted.
* In hardlink mode, deleting one directory entry leaves data alive while another link
  exists.
* In shared mode, a shared blob is deleted only after no repository graph, lease, or
  pin reaches it.

After GC, stale bbolt entries are removed or rebuilt. bbolt reuses freed pages but
does not automatically shrink its file; maintenance may compact it when worthwhile.

## Measured Direction

Storage workload: 16 repositories, 320 blob references, 80 unique 256-KiB blobs, and
75% shared references.

| Mode | Ingest | Lookup | Physical space | Payload inodes |
|---|---:|---:|---:|---:|
| Portable copies | 35.22 ms | 6.388 us/op | 80.32 MiB | 320 |
| Hardlink | 12.13 ms | 6.187 us/op | 20.32 MiB | 80 |
| Global CAS plus layout hardlinks | 14.07 ms | 6.109 us/op | 20.61 MiB | 80 |

Hardlink mode reduced physical allocation by 74.7% and used the same number of
payload inodes as the global-CAS prototype. Lookup time differed by only 1.3%.
That prototype still created hardlinks into repository layouts, so it is not a direct
measurement of the no-hardlink shared mode. Shared mode requires a separate target
hardware measurement.

For a 50,000-record dedupe cache:

| Metric | bbolt | SQLite WAL |
|---|---:|---:|
| Digest lookup | 2.655 us | 18.575 us |
| Durable add | 473 us | 443 us |
| Durable update | 435 us | 285 us |
| Durable delete | 468 us | 388 us |
| Database size | 32.39 MiB | 19.34 MiB |
| Stripped binary | 2.54 MiB | 6.46 MiB |
| Peak RSS during load | 97.59 MiB | 45.27 MiB |

bbolt was about 7 times faster for the dominant digest lookup and produced a binary
3.91 MiB smaller. SQLite used less database space and memory and had faster writes.
bbolt is selected because this state is a simple, read-heavy, rebuildable cache.

These measurements are directional. Filesystem, flash media, CPU architecture, cache
size, and kernel behavior can change the result.

## Existing Systems

| System | Storage model | Relevance to Satellite |
|---|---|---|
| Docker and Podman | OCI blobs plus runtime snapshots | Execution stores, not registries |
| containerd | Global CAS, bbolt, and snapshots | Shared-mode reference; snapshots are unnecessary |
| Distribution | Global CAS plus repository links | Dedupes, but is not an OCI layout |
| Zot | Repository blobs, hardlinks, and Bolt | Hardlink-mode reference; broader registry |
| olareg | OCI layouts with lightweight metadata | Closest reference for portable mode |

Satellite stores registry transport content only. It does not need unpacked container
filesystems or runtime snapshots.

## Why Not Other Stores

* SQLite is better for relational data, but that is not the current cache workload.
* etcd embeds networking, MVCC, Raft, WALs, and snapshots while using bbolt
  underneath; it solves distributed consensus, not local dedupe.
* Badger and Pebble favor large or write-heavy LSM workloads and add compaction and
  multi-file management.
* A separate global CAS is represented by shared mode, not forced on portable or
  hardlink deployments.
* S3 and other object stores are future multi-node backends, not the default edge
  store.

## Storage Interface

Registry handlers depend on behavior, not paths:

```go
type RepositoryStore interface {
    Resolve(context.Context, Repository, Reference) (Descriptor, error)
    Open(context.Context, Repository, Descriptor) (ReadSeekCloser, error)
    Begin(context.Context, Repository, ExpectedContent) (Writer, error)
    Commit(context.Context, Repository, Writer) (Descriptor, error)
    Publish(context.Context, Repository, ReferenceUpdate) error
    Referrers(context.Context, Repository, Digest, string) ([]Descriptor, error)
}
```

Storage mode selection and bbolt access remain behind this interface.

## Consequences

* Good: bbolt provides fast persistent dedupe lookup with a small binary.
* Good: Portable and hardlink modes preserve complete OCI layouts.
* Good: Shared mode deduplicates when hardlinks are unavailable.
* Good: Artifact bytes remain recoverable without the bbolt file.
* Neutral: Three modes require a clear compatibility and export story.
* Neutral: bbolt must be versioned, bounded, checked, and occasionally compacted.
* Bad: Hardlinks are not universal and cannot cross filesystems.
* Bad: Shared mode is not a standalone OCI layout until materialized.
* Bad: Recovery and GC must reconcile filesystem truth with cached metadata.

## Validation

* Verify digest and size before every commit.
* Test concurrent commits of the same digest in every mode.
* Inject crashes before and after file, root, and bbolt commits.
* Delete and corrupt bbolt and confirm deterministic recovery.
* Test stale cached paths and deletion of one hardlink source.
* Test filesystems without hardlinks and explicit fallback behavior.
* Confirm GC with indexes, subjects, referrers, leases, and pins.
* Import and export portable and hardlink layouts with OCI tools.
* Materialize a shared repository and validate the exported OCI layout.
* Recheck performance and flash writes on ARM64/eMMC and SD-card targets.

## References

* [OCI Image Layout](https://github.com/opencontainers/image-spec/blob/v1.1.1/image-layout.md)
* [OCI Image Manifest](https://github.com/opencontainers/image-spec/blob/v1.1.1/manifest.md)
* [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md)
* [bbolt](https://github.com/etcd-io/bbolt)
* [Zot storage configuration](https://github.com/project-zot/zot/blob/main/examples/README.md#storage)
* [olareg](https://github.com/olareg/olareg)
