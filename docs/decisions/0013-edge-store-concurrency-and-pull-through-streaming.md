---
status: proposed
date: 2026-10-15
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Edge store concurrency and pull-through streaming

## Context

The satellite keeps replicated content in an ORAS OCI image layout and serves it through the loopback proxy ([ADR-0009](0009-transparent-oci-registry-proxy.md)). Plan item 5 (edge pull path) found these limits at `508b0d6`, with oras-go v2.6.2:

* **One lock serializes the store.** `OCIStore` has one `sync.RWMutex` (`internal/satellite/store/oci.go:24`). `Pull` and `Fetch` take the read lock (`:44`, `:96`), and `Fetch` keeps it until the client closes the body. `Replicate` and `Delete` take the write lock for the whole copy or delete (`:115`, `:159`). A replication of a large image therefore blocks every local pull, and one slow client blocks replication.
* **Pull-through copies the whole graph first.** On a proxy-mode miss, `proxy/process/image/pull.go` replicates the full manifest graph before it writes the first byte, with a 30-minute limit (`pull.go:22,78,97`). The fill is deduplicated per resource, repository and reference (`pull.go:73`), not per digest, so two repositories sharing a layer download it twice.
* **`index.json` is written in place.** oras-go saves the index with `os.WriteFile` (`content/oci/oci.go:461-467`) after every tag change. A crash during that write leaves a torn file, and `oci.New` then fails with `failed to decode index file`, so the satellite does not start (`cmd/satellite/main.go:674`).
* **GC deletes in-flight copies.** oras `Store.GC` keeps only graphs reachable from name tags (`oci.go:474`, `:529-583`). `oras.Copy` tags the root after its graph is pushed (`copy.go:476-479`), so a GC during a fill removes layers the copy has already pushed. GC also never looks at `ingest/`, where a crash leaves partial files.
* **oras-go's own locking is narrower.** `Fetch`, `Push` and `Tag` take only oras's read lock (`oci.go:125-148`, `:236-240`), and a pushed blob becomes visible by rename after it is verified (`content/oci/storage.go:89-99`). Readers by digest do not need a store-wide lock.

## Decision Drivers

* A pull of retained content stays fast while replication or GC runs (plan fitness test 0.4, `TestOCIStoreServesRetainedContentDuringReplication`).
* A crash at any point leaves a store the satellite can open.
* The store only ever tags complete graphs; replication, GC and peers ([ADR-0010](0010-peer-to-peer-distribution.md)) rely on that.
* A client gets a manifest without waiting for every layer.

## Decision

1. **Lock model.** Reads by digest take no store lock; oras `Fetch` is safe next to `Push` and `Tag`. Index mutations (tag, untag) take a short exclusive lock. The store-wide lock that `Fetch` holds until close goes away; open readers are counted per digest instead (`RefCounter`).
2. **Atomic index.** The store opens oras with `AutoSaveIndex=false` and persists `index.json` itself after every tag change: temporary file in the store root, fsync, rename, directory fsync (`WriteIndexAtomic`, built from the tags by `BuildIndex`).
3. **Torn-index recovery.** `OpenOCIStore` with `RecoverIndex` moves an undecodable `index.json` aside and opens an empty index. The satellite then forgets its replicated entities (`ResetReplicatedEntities`) so the next state tick replicates every group again. Blobs stay on disk, so re-tagging is cheap. There is no last-good copy of the index: new writes are atomic, so a torn file can only come from an older version.
4. **Garbage collection.** Our own mark and sweep (`CollectGarbage`) replaces oras `Store.GC`. Roots are the tagged graphs plus `GCRoots`, at least the fill leases that record what in-flight copies pushed before they tag. Unreferenced content is deleted only after a grace period and when no reader holds it. Old files in `ingest/` are removed. `Delete` untags an artifact only while its tag still resolves to the artifact's digest (D9) and then runs this collection in place of oras `Store.GC`, so the content deletion itself is always the collector's.
5. **Pull-through streaming.** On a manifest miss in proxy mode the satellite stores and returns the manifest, then prefetches the graph in a bounded lane (`internal/satellite/lane`). A blob miss is streamed from upstream while it is written to the store, and committed only after its digest verifies. The tag is set only when the graph is complete. Fills are deduplicated per digest. `PROXY_STREAMING=false`, the default until the implementation is proven, keeps today's whole-graph fill.
6. **Server limits.** The proxy sets a write deadline per write (`proxy.WithWriteDeadline`, `PROXY_WRITE_TIMEOUT`) instead of a server `WriteTimeout`, which would cut long blob downloads. `PROXY_MAX_CONNECTIONS` caps connections with `golang.org/x/net/netutil.LimitListener` on the proxy listener.

## Consequences

* Good: local pulls no longer wait for replication, and replication no longer waits for slow clients.
* Good: the satellite starts after a crash during an index write, at the cost of one full resync.
* Good: GC cannot break a running copy or a running download.
* Bad: the store owns index persistence and GC, which oras-go did before. We propose an atomic `SaveIndex` upstream to oras-go and drop our writer if it lands.
* Bad: more moving parts on the pull path (leases, reference counts, a prefetch lane). Every store change runs `task e2e-crash-recovery`.
* Peers ([ADR-0010](0010-peer-to-peer-distribution.md)) depend on the complete-graph invariant of point 5.

## Implementation Status

| Part | Status |
|---|---|
| ADR | proposed |
| `OpenOCIStore`, `WriteIndexAtomic`, `BuildIndex`, `ResetReplicatedEntities` (5.1) | skeleton, pending tests |
| `CollectGarbage`, `FillLeases`, `RefCounter` (5.2) | skeleton, pending tests |
| `lane`, `NewPullWithOptions`, `NewLanePrefetcher`, `Filler` (5.3) | skeleton, pending tests |
| `WithWriteDeadline`, `PROXY_*` settings (5.4) | skeleton, pending test; settings parsed, not read |

## Validation

* `internal/satellite/store` `TestOCIStoreServesRetainedContentDuringReplication` (fitness test 0.4) passes without its pending marker.
* The pending tests of plan item 5 in `internal/satellite/store`, `internal/satellite/state`, `internal/satellite/lane`, `internal/satellite/proxy` and `internal/satellite/proxy/process/image` pass without their markers.
* `task e2e-crash-recovery` stays green.
