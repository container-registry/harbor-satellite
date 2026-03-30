---
status: proposed
date: 2026-03-29
deciders: [Harbor Satellite Development Team]
consulted: [Harbor Satellite Users, Architects, Edge Computing Operators]
informed: [Harbor Satellite Developers, Operators]
---

# Air-Gapped Satellite via Virtual Twin and Signed Bundles

## Context and Problem Statement

Harbor Satellite requires live connectivity to Ground Control (GC) and Harbor. Air-gapped environments (military, classified networks, critical infrastructure, disconnected edge) cannot use it. The README lists "Air-gapped capable" as a goal, but no implementation exists.

Operators managing both connected and air-gapped sites today need two completely different toolchains (e.g., Harbor Satellite for connected, Zarf for air-gapped). This doubles operational complexity and prevents unified fleet management.

## Decision Drivers

- Unified workflow: same GC API and satellite binary for both connected and air-gapped
- Cryptographic trust: bundles must be signed and verifiable — better than "hand someone a USB stick and hope"
- Seamless reconciliation: when an air-gapped site comes online, it should sync with no migration or data loss
- Differential updates: avoid re-shipping unchanged images on every update
- OCI-native: reuse existing OCI tooling and formats (go-containerregistry, Cosign, Zot)
- Match or exceed Zarf capabilities while adding fleet management and reconciliation

## Considered Options

- Option 1: Zarf integration — use Zarf as a dependency for bundle generation
- Option 2: Virtual Twin with OCI Image Layout bundles (native)
- Option 3: Registry-to-registry export (oras/skopeo sync to tarball)

## Decision Outcome

Chosen option: "Virtual Twin with OCI Image Layout bundles", because it provides a unified management model (same GC API for connected and air-gapped), leverages existing codebase patterns (Replicator interface, PersistedState format, Cosign signing), and adds reconciliation — something Zarf does not support. Zarf integration would introduce a large external dependency without adding fleet management. Registry-to-registry export lacks signing, state tracking, and differential support.

### Consequences

- Good: One workflow for connected and air-gapped — same GC API, same satellite binary
- Good: Cryptographically signed bundles with configurable trust (OOB or TOFU)
- Good: Seamless reconciliation when air-gapped satellite comes online (state.json format compatibility)
- Good: Differential bundles minimize physical transport size
- Good: Reuses existing Replicator interface — LayoutReplicator is a drop-in implementation
- Good: OCI Image Layout is the same format Zarf uses — interoperable with ecosystem
- Neutral: Requires Cosign dependency in Ground Control module
- Bad: Bundle generation can be slow for large image sets (pulling from Harbor)
- Bad: Multi-GB bundles may need splitting strategy (deferred to later phase)

---

## Zarf Analysis

[Zarf](https://github.com/zarf-dev/zarf) is the gold standard for air-gapped K8s deployments. It bundles container images, Helm charts, manifests, and init scripts into a single tarball using OCI Image Layout format.

### What Zarf Does Well

- Declarative packaging via `zarf.yaml`
- OCI-native image storage with content-addressed blob deduplication
- Cosign signing and verification
- Differential packages (`--differential` excludes images already in a base package)
- Syft-generated SBOMs in every package
- Image injection into cluster registries with reference rewriting

### Where We Go Beyond Zarf

- **Fleet management**: Zarf is a CLI tool per-cluster. GC manages hundreds of air-gapped twins from a single control plane.
- **Reconciliation**: Zarf is air-gapped only. We unify connected and disconnected into one lifecycle — an air-gapped satellite can come online and immediately sync.
- **Automatic state tracking**: Zarf requires manual re-packaging for every change. Our virtual twin tracks desired state and generates bundles with full change awareness.
- **Trust chain depth**: Our trust extends from SPIFFE/hardware identity through device-bound encryption to bundle signing. Zarf has Cosign on the tarball only.
- **CRI integration**: Satellite already configures containerd/Docker/CRI-O/Podman as mirrors. Zarf only injects into cluster registries.

### Feature Mapping

| Zarf | Harbor Satellite Equivalent |
|---|---|
| `zarf.yaml` | GC group/config definitions (the declaration already exists) |
| OCI Image Layout tarball | Same format — `oci/` directory in bundle |
| `cosign sign` | Cosign signature on `bundle.json` manifest |
| `--differential` | Differential bundles via base chain |
| `sboms/` directory | Phase 2 — Syft integration |
| Image injection into registry | `LayoutReplicator` pushes into local Zot |

---

## Diagrams & User Flows

### System Architecture: Connected vs Air-Gapped

```
  "CLOUD / CONNECTED SIDE"
 +----------------------------------------------------+
 |                                                    |
 | +-----------+   +--------------+   +-------------+ |
 | | "Harbor"  |-->| "Ground"     |-->| "Bundle"    | |
 | | "Registry"|<--| "Control"    |   | "Store"     | |
 | |           |   |              |   | "(Disk/S3)" | |
 | | +-------+ |   | +----------+|   +------+------+ |
 | | |"Images| |   | |"Virtual" ||          |        |
 | | |"State"| |   | |"Twins"   ||     "presigned"   |
 | | |"Artfs"| |   | +----------+|       "URLs"      |
 | | +-------+ |   |             |          |        |
 | +-----+-----+   +--+-------+--+          |        |
 |       |            |       |              |        |
 +-------+------------+-------+--------------+--------+
          ^            ^       ^              |
          |            |       |              |
     "pull images"     |  "register,"        "download"
     "and state"       |  "status,"          ".tar.gz"
     "artifacts"       |  "heartbeat"             |
          |            |       |              |
          |            |       |              |
 +--------+--+   +----+-------+-+   +--------+---+
 |"Connected" |   | "Connected"  |   | "Operator"  |
 |"Satellite" |   | "Satellite"  |   |"Workstation"|
 |"Site A"    |   | "Site B"     |   +------+------+
 |            |   |              |          |
 | +--------+ |   | +--------+  |   "physical"
 | | "Zot"  | |   | | "Zot"  |  |   "transport"
 | +--------+ |   | +--------+  |   "(USB/DVD)"
 +-------------+   +--------------+          |
                                             |
   "Satellites only make"                    |
   "outbound connections"                    |
   "(to Harbor and GC)"                     |
                                             |
 - - - - - - "AIR GAP" - - - - - - -+- - - -
                                     |
                              +------+------+
                              | "Air-Gapped"|
                              | "Satellite" |
                              | "Site C"    |
                              |             |
                              | +--------+  |
                              | | "Zot"  |  |
                              | +--------+  |
                              | +--------+  |
                              | |"bundle"|  |
                              | |"watcher"|  |
                              | +--------+  |
                              +-------------+
```

**Data flow:**
- Harbor pushes artifact metadata to GC (`POST /api/groups/sync`)
- GC pushes state artifacts back into Harbor
- Satellites only make outbound connections:
  - To Harbor: pull state artifacts + container images
  - To GC: ZTR registration, heartbeat, status
- Bundle generation (new): GC pulls images from Harbor

### Flow 1: Day-Zero Setup (Air-Gapped Satellite)

**Fleet Admin** manages sites from GC.
**Site Operator** is at the air-gapped site.

```
 "Fleet Admin"     "Ground Control"    "Site Operator"
       |                  |                   |
       | "1. Register"    |                   |
       | "POST /api/sats" |                   |
       | "{mode:airgapped}"|                   |
       |----------------->|                   |
       |                  |-- "create twin"   |
       |                  |-- "create robot"  |
       |                  |                   |
       | "2. Generate"    |                   |
       | "POST .../bundles"|                   |
       | "{type: full}"   |                   |
       |----------------->|                   |
       |                  |-- "pull Harbor"   |
       |                  |-- "OCI layout"    |
       |                  |-- "sign + tar.gz" |
       |<-"presigned URL"-|                   |
       |                  |                   |
       | "3. Download"    |                   |
       |----------------->|                   |
       |<-"bundle.tar.gz"-|                   |
       |                  |                   |
       | "4. Export key"   |                   |
       |----------------->|                   |
       |<--"public key"---|                   |
       |                  |                   |
       |                                      |
       | "5. USB: bundle.tar.gz,"             |
       |    "cosign.pub, satellite binary"    |
       |        "...physical transport..."    |
       |                                      |
       |                    "6. Deploy:"      |
       |                    "install binary," |
       |                    "place key,"      |
       |                    "place bundle"    |
       |                                      |
       |                    "7. Start:"       |
       |                    "$ satellite"     |
       |                    "  --air-gapped"  |
       |                    "  --bundle-watch" |
       |                    "  --verify-key"  |
       |                                      |
       |                    "-- verify sig"   |
       |                    "-- import to Zot"|
       |                    "-- write state"  |
       |                                      |
       |                    "8. Workloads"    |
       |                    "pull from Zot"   |
       v                                      v
```

### Flow 2: Ongoing Updates (Differential)

```
 "Fleet Admin"     "Ground Control"    "Site Operator"
       |                  |                   |
       | "1. Update group" |                   |
       | "POST groups/sync"|                   |
       |----------------->|                   |
       |                  |-- "update state"  |
       |                  |   "in Harbor"     |
       |                  |                   |
       | "2. Gen diff"    |                   |
       | "POST .../bundles"|                   |
       | "{differential}" |                   |
       |----------------->|                   |
       |                  |-- "diff manifests"|
       |                  |-- "pull 3 images" |
       |                  |-- "sign + tar.gz" |
       |<-"presigned URL"-|                   |
       |                  |                   |
       | "3. Download + transport"            |
       | "(much smaller than full)"           |
       |  - - - "physical" - - - - - - - - ->|
       |                  |                   |
       |                  |   "4. Drop in"    |
       |                  |   "watch dir"     |
       |                  |                   |
       |                  |   "5. Auto-import"|
       |                  |   "-- verify sig" |
       |                  |   "-- import 3"   |
       |                  |   "-- delete 1"   |
       |                  |   "-- update state"|
       |                  |   "-- mv processed"|
       |                  |                   |
       | "6. Mark applied" |                   |
       | "(optional)"     |                   |
       |----------------->|                   |
       |                  |-- "record"        |
       |                  |-- "enable next"   |
       |                  |   "differential"  |
       v                  v                   v
```

### Flow 3: Reconciliation (Air-Gapped to Connected)

```
 "Fleet Admin"     "Ground Control"    "Satellite"
       |                  |                   |
       |                  |    "Currently"    |
       |                  |    "air-gapped,"  |
       |                  |    "25 images,"   |
       |                  |    "state.json"   |
       |                  |    "up to date"   |
       |                  |                   |
       | "1. Switch mode"  |                   |
       | "{mode:connected}"|                   |
       |----------------->|                   |
       |                  |-- "expects"       |
       |                  |   "heartbeats"    |
       |                  |                   |
       |                  |   "2. Reconfig"   |
       |                  |   "$ satellite"   |
       |                  |   "  --token abc" |
       |                  |   "  --gc-url .." |
       |                  |                   |
       |                  |<- "3. ZTR reg" ---|
       |                  |- "creds + URL" -->|
       |                  |                   |
       |                  |   "4. State sync" |
       |                  |   "loads state"   |
       |                  |   "from bundle"   |
       |                  |                   |
       |                  |   "25 match: skip"|
       |                  |   "3 new: pull"   |
       |                  |   "1 removed: del"|
       |                  |                   |
       |                  |<-"5. Heartbeat"---|
       |                  |                   |
       |                  |   "6. Polls"      |
       |                  |   "normally"      |
       v                  v                   v
```

All satellite connections are outbound (to GC for
ZTR/heartbeat, to Harbor for image pull).

### Flow 4: Trust Verification

```
  "OOB Trust Mode"             "TOFU Trust Mode"
  "(out-of-band)"              "(trust on first use)"

 +---------------------------+ +---------------------------+
 | "BEFORE FIRST BUNDLE"     | | "FIRST BUNDLE"            |
 |                           | |                           |
 | "Admin exports public"    | | "bundle.json contains"    |
 | "key from GC, delivers"   | | "verification_key field"  |
 | "separately to satellite" | |                           |
 |                           | | "Satellite has no key"    |
 | "satellite"               | | "-> accepts from bundle"  |
 | "  --bundle-verify-key"   | | "-> pins it locally"      |
 | "  /etc/sat/cosign.pub"   | | "-> logs WARNING: TOFU"   |
 |                           | |                           |
 +-------------+-------------+ +-------------+-------------+
               |                              |
               v                              v
 +---------------------------+ +---------------------------+
 | "EVERY BUNDLE"            | | "SUBSEQUENT BUNDLES"      |
 |                           | |                           |
 | "1. Read bundle.sig"      | | "1. Read bundle.sig"      |
 | "2. Read bundle.json"     | | "2. Read bundle.json"     |
 | "3. Verify sig against"   | | "3. Verify sig against"   |
 |    "pre-loaded key"       | |    "pinned key"           |
 | "4. Pass: proceed"        | | "4. Pass: proceed"        |
 |    "Fail: reject"         | |    "Fail: reject"         |
 |                           | |                           |
 | "Trust: STRONG"           | | "Trust: MODERATE"         |
 | "(key never came from"    | | "(first bundle unverified"|
 |  "an unverified source)"  | |  "subsequent verified)"   |
 +---------------------------+ +---------------------------+
```

### Flow 5: Operator Experience Side by Side

**Connected vs Air-Gapped** — same GC API, same binary.

| Stage | Connected | Air-Gapped |
|---|---|---|
| Register | `POST /api/satellites {mode:"connected"}` | `POST /api/satellites {mode:"airgapped"}` |
| Groups | Same API | Same API |
| Deploy | `satellite --token --gc-url` | Generate bundle, transport, `satellite --air-gapped --bundle-watch-dir` |
| Updates | Auto-sync, nothing to do | Generate diff bundle, drop in watch dir |
| Workloads | Pull from local Zot | Pull from local Zot (identical) |
| Monitoring | Heartbeats to GC | Dashboard shows last bundle applied |
| Come online | Already online | Remove `--air-gapped`, add `--token`. Auto-syncs. |

### Data Flow: Bundle Generation (Ground Control)

In normal operation, GC only handles metadata. Bundle
generation is new: GC pulls actual images from Harbor.

```
 "Harbor"               "Ground Control"
 +-----------+    +-------------------------------+
 |           |    |                               |
 | +-------+ |    | "POST .../bundles"            |
 | |"nginx"| |    |                               |
 | |"redis"| |    | "1. Resolve desired state"    |
 | |"app"  | |    |    "satellite -> groups"      |
 | +-------+ |    |    "groups -> artifact list"  |
 |           |    |                               |
 |    "GC pulls"  | "2. If differential:"         |
 |    "images"    |    "diff against last bundle" |
 |    "for"  |    |    "keep only new+changed"    |
 |    "bundle"    |                               |
 |    "gen"  |    | "3. Build OCI Image Layout"   |
 |           |    |    "v1.Image -> layout.Write"  |
 |   ------->|    |    "blobs dedup by digest"    |
 |           |    |                               |
 +-----------+    | "4. Write state files"         |
                  |    "same PersistedState format"|
                  |                               |
                  | "5. Sign bundle.json"          |
                  |    "-> bundle.sig (cosign)"    |
                  |                               |
                  | "6. tar.gz + store + record"   |
                  |    "-> presigned URL (30min)"  |
                  +-------------------------------+
```

### Data Flow: Bundle Import (Satellite)

```
 "Air-Gapped Satellite"
 +----------------------------------------------------+
 |                                                    |
 | "/bundles/bundle.tar.gz" "<- placed by operator"   |
 |                                                    |
 | "1. BundleImportProcess detects new file"          |
 | "2. Extract to temp dir"                           |
 |                                                    |
 | "3. VERIFY"                                        |
 |    "bundle.json + bundle.sig"                      |
 |    "verify against cosign.pub"                     |
 |    "FAIL -> reject    PASS -> continue"            |
 |                                                    |
 | "4. Parse state files"                             |
 |    "-> Entity[] list (same as connected)"          |
 |                                                    |
 | "5. Import images"           +-----------+         |
 |    "LayoutReplicator"        |           |         |
 |    "layout.Image(digest)"    |  "Zot"    |         |
 |    "remote.Write(img,ref)"-->| "(local)" |         |
 |                              |           |         |
 |    "Same Replicator"         +-----------+         |
 |    "interface as"                                  |
 |    "BasicReplicator"                               |
 |                                                    |
 | "6. Removals (differential)"                       |
 |    "crane.Delete removed images"                   |
 |                                                    |
 | "7. Persist state"                                 |
 |    "SaveState(state.json, entities)"               |
 |    "SAME FORMAT as connected mode"                 |
 |    "<- makes reconciliation seamless"              |
 |                                                    |
 | "8. Cleanup"                                       |
 |    "mv bundle.tar.gz -> processed/"                |
 +----------------------------------------------------+
```

---

## Technical Design

### 1. Virtual Twin Model

A virtual twin is a satellite record in GC with `mode = 'airgapped'`. It shares the same tables, group assignments, config assignments, and robot accounts as connected satellites. The only behavioral difference: GC does not expect it to phone home, and exposes bundle generation APIs for it.

**Why a column, not a separate table:** Eliminates duplication of group/config/robot account logic. The mode is a behavioral flag, not a structural difference. All existing satellite management APIs work unchanged.

#### Database Changes

New migration `016_satellite_mode_and_bundles.sql`:

```sql
-- Satellite mode
ALTER TABLE satellites ADD COLUMN mode VARCHAR(20) NOT NULL DEFAULT 'connected';
ALTER TABLE satellites ADD CONSTRAINT chk_satellite_mode
  CHECK (mode IN ('connected', 'airgapped'));

-- Bundle tracking
CREATE TABLE bundles (
    id                SERIAL PRIMARY KEY,
    satellite_id      INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
    bundle_type       VARCHAR(20) NOT NULL CHECK (bundle_type IN ('full', 'differential')),
    sequence          INT NOT NULL,
    requires_sequence INT,
    state_digest      VARCHAR(255) NOT NULL,
    config_digest     VARCHAR(255) NOT NULL,
    group_digests     JSONB NOT NULL,
    artifact_manifest JSONB NOT NULL,
    signature         TEXT NOT NULL,
    signing_key_id    VARCHAR(255) NOT NULL,
    size_bytes        BIGINT NOT NULL,
    storage_path      TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMP,
    applied_at        TIMESTAMP,
    notes             TEXT
);
CREATE INDEX idx_bundles_satellite ON bundles(satellite_id, created_at DESC);

-- Signing keys
CREATE TABLE bundle_signing_keys (
    id          SERIAL PRIMARY KEY,
    key_id      VARCHAR(255) UNIQUE NOT NULL,
    public_key  TEXT NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMP
);
```

### 2. Bundle Format

A bundle is a portable snapshot of exactly what the satellite would normally pull from Harbor. It reuses the same state OCI artifact and state JSON formats that connected satellites already consume — the bundle just packages them alongside the actual container images so everything is available offline.

```
bundle.tar.gz
├── bundle.json              # Manifest: metadata, signing info, bundle type
├── bundle.sig               # Cosign detached signature of bundle.json
├── state/                   # Same state artifacts the satellite pulls from Harbor
│   ├── satellite-state.json # SatelliteStateArtifact (group URLs, config URL)
│   ├── groups/
│   │   └── <group>.json     # StateArtifact per group (artifacts.json content)
│   └── config.json          # Satellite config (app_config + zot_config)
├── oci/                     # OCI Image Layout with actual container images
│   ├── oci-layout           # {"imageLayoutVersion": "1.0.0"}
│   ├── index.json           # OCI Image Index
│   └── blobs/sha256/        # Content-addressed blobs (layer dedup automatic)
└── sbom/                    # (Phase 2) Per-image SBOMs via Syft
    └── <digest>.spdx.json
```

**Key design principle:** The `state/` directory contains the exact same JSON that the satellite would receive by pulling state OCI artifacts from Harbor. The `groups/<group>.json` files are the same `artifacts.json` content that `CreateStateArtifact()` embeds into OCI images. This means the satellite's state parsing code works unchanged — the only difference is the source (local file vs OCI pull).

**Full vs differential bundles:**
- **Full bundle:** Contains all state files + all container images. Used for first deployment or to reset the chain. Larger but self-contained. Can be applied at any time regardless of current state.
- **Differential bundle:** Contains updated state files + only changed/added container images. The `bundle.json.differential` field lists removals. Much smaller for incremental updates — critical when physically transporting media.

**Sequential application:** Differential bundles form a strict chain. Each differential bundle records a `sequence` number and a `requires_sequence` referencing the previous bundle. The satellite tracks the last applied sequence number and rejects any differential bundle whose `requires_sequence` does not match. This guarantees state consistency — you cannot skip a bundle in the chain. If a bundle is lost or skipped, the operator must generate a new full bundle to reset the chain. Full bundles always reset the sequence (they have no `requires_sequence`).

**Why OCI Image Layout:** `go-containerregistry/pkg/v1/layout` is already available (v0.20.3 in go.mod). Layer deduplication is automatic via content-addressed blobs. Same format Zarf uses.

#### bundle.json Schema

```json
{
  "version": 1,
  "satellite_name": "edge-site-01",
  "bundle_type": "full",
  "sequence": 1,
  "requires_sequence": null,
  "created_at": "2026-03-29T10:00:00Z",
  "ground_control_url": "https://gc.example.com",
  "state_digest": "sha256:...",
  "config_digest": "sha256:...",
  "signing_key_id": "key-2026-03",
  "trust_mode": "oob",
  "verification_key": null,
  "groups": [
    {"name": "production-images", "digest": "sha256:...", "artifact_count": 12}
  ],
  "artifacts": [
    {
      "repository": "library/nginx",
      "tag": "1.25",
      "digest": "sha256:...",
      "size_bytes": 51234567,
      "type": "IMAGE",
      "oci_layout_digest": "sha256:..."
    }
  ],
  "differential": null
}
```

**Sequence fields:**
- `sequence`: Monotonically increasing per satellite. GC assigns it on bundle generation.
- `requires_sequence`: For differential bundles, the sequence number of the bundle that must be applied before this one. Null for full bundles.

Example chain:
1. Full bundle: `sequence: 1, requires_sequence: null` — can always be applied
2. Diff bundle: `sequence: 2, requires_sequence: 1` — requires bundle 1
3. Diff bundle: `sequence: 3, requires_sequence: 2` — requires bundle 2
4. If bundle 2 is lost: generate a new full bundle (`sequence: 4, requires_sequence: null`) to reset

The satellite stores `last_applied_sequence` locally. On import:
- Full bundle: always accepted, resets `last_applied_sequence`
- Differential bundle: rejected if `requires_sequence != last_applied_sequence`

For differential bundles, the `differential` field contains:

```json
{
  "added": ["library/nginx:1.26"],
  "updated": ["library/redis:7.2"],
  "removed": ["library/redis:7.0"]
}
```

For TOFU trust mode, the first bundle includes `verification_key` with the PEM-encoded public key. Subsequent bundles omit it.

### 3. Bundle Storage and Download

Abstracted storage backend: local disk by default, S3-compatible object storage when configured. Both produce time-limited presigned download URLs with 30-minute expiry.

```go
type BundleStorage interface {
    Store(ctx context.Context, bundleID int, reader io.Reader) (storagePath string, err error)
    GenerateDownloadURL(ctx context.Context, storagePath string, expiry time.Duration) (string, error)
    Delete(ctx context.Context, storagePath string) error
}
```

**Local disk:** Bundles stored at `{BUNDLE_STORAGE_DIR}/bundles/{satellite}/{bundle_id}.tar.gz`. Download URL is an HMAC-signed GC API endpoint with 30-minute expiry.

**S3:** Standard S3 presigned URLs with 30-minute TTL.

Configuration via environment variables:

```
BUNDLE_STORAGE_BACKEND=local|s3
BUNDLE_STORAGE_DIR=/var/lib/gc/bundles
BUNDLE_S3_BUCKET=
BUNDLE_S3_PREFIX=bundles
BUNDLE_S3_REGION=
BUNDLE_S3_ENDPOINT=
BUNDLE_DOWNLOAD_EXPIRY=30m
```

Bundles are generated on-demand only (no auto-generation).

### 4. Bundle Generation (Ground Control)

New package: `ground-control/internal/bundle/`

Note: In normal connected operation, GC only handles metadata — it receives artifact lists via `POST /api/groups/sync` and pushes state OCI artifacts into Harbor. Satellites pull images directly from Harbor. Bundle generation is a new capability where GC must also pull actual container images from Harbor to package them into the OCI Image Layout for offline transport.

Generation flow:

1. **Resolve desired state**: Fetch satellite's groups and config. Reuse `AssembleGroupState()` from `ground-control/internal/utils/helper.go`.
2. **Compute differential** (if applicable): Load last applied bundle's `artifact_manifest` from DB. Diff against current artifact list.
3. **Pull images from Harbor into OCI layout**: `crane.Pull()` from Harbor registry to get `v1.Image`, then `layout.Write()` into `oci/` directory. This is new — GC normally only pushes state artifacts to Harbor, it does not pull container images. Bundle generation requires GC to have read access to the actual images. Blobs dedup automatically by digest.
4. **Write state files**: Same JSON format as existing state artifacts in Harbor.
5. **Build bundle.json**: Assemble manifest with all metadata.
6. **Sign**: Cosign library signs `bundle.json`, produces `bundle.sig`.
7. **Create tarball**: `tar.gz` the directory.
8. **Store**: Write to configured storage backend.
9. **Record**: Insert into `bundles` table.
10. **Return**: Bundle metadata + presigned download URL.

#### API Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| PATCH | `/api/satellites/{name}` | Admin | Update mode (connected/airgapped) |
| POST | `/api/satellites/{name}/bundles` | Admin | Generate bundle |
| GET | `/api/satellites/{name}/bundles` | Admin | List bundles |
| GET | `/api/satellites/{name}/bundles/{id}` | Admin | Bundle metadata + presigned URL |
| GET | `/api/satellites/{name}/bundles/{id}/download` | Presigned | Stream tarball |
| POST | `/api/satellites/{name}/bundles/{id}/applied` | Admin | Mark as applied |
| POST | `/api/signing-keys` | SysAdmin | Register signing key |
| GET | `/api/signing-keys` | Admin | List active keys |
| DELETE | `/api/signing-keys/{id}` | SysAdmin | Revoke key |

### 5. Bundle Import (Satellite)

New package: `internal/bundle/`

#### LayoutReplicator

The existing `Replicator` interface in `internal/state/replicator.go`:

```go
type Replicator interface {
    Replicate(ctx context.Context, replicationEntities []Entity) error
    DeleteReplicationEntity(ctx context.Context, replicationEntity []Entity) error
}
```

A new `LayoutReplicator` implements this interface reading from a local OCI Image Layout instead of a remote registry:

```go
type LayoutReplicator struct {
    layoutPath     layout.Path
    remoteURL      string
    remoteUsername string
    remotePassword string
    useUnsecure    bool
}
```

The `Replicate` method reads `v1.Image` from `layout.Path` and pushes to local Zot via `remote.Write`. This is the architectural linchpin: the entire `FetchAndReplicateStateProcess` orchestration works unchanged because we swap only the replicator implementation.

#### New CLI Flags

```
--air-gapped                  Disable GC polling, enable bundle import mode
--bundle-path <path>          Import specific bundle on startup
--bundle-watch-dir <dir>      Watch directory for new .tar.gz bundles
--bundle-verify-key <path>    Cosign public key for verification (OOB trust mode)
--bundle-trust-mode <mode>    oob | tofu (default: oob)
```

#### BundleImportProcess

Implements `scheduler.Process`:

```go
type BundleImportProcess struct {
    watchDir      string
    verifyKeyPath string
    trustMode     string
    cm            *config.ConfigManager
    stateFilePath string
}
```

In air-gapped mode, the satellite skips ZTR, state replication, and heartbeat schedulers. It starts only Zot and the BundleImportProcess.

### 6. Reconciliation

When an air-gapped satellite comes online:

1. Operator removes `--air-gapped`, adds `--token` + `--ground-control-url`
2. Operator updates GC: `PATCH /api/satellites/{name} {"mode": "connected"}`
3. Satellite runs ZTR, gets credentials, starts state replication
4. State replication loads `state.json` written by last bundle import (same `PersistedState` format)
5. `GetChanges()` compares persisted entities against remote state: matching digests are skipped, new images are pulled, removed images are deleted
6. Satellite is fully synced with no migration needed

This works because `PersistedState` is the contract between bundle import and connected replication. Both write the same format.

### 7. Trust Chain

#### Two Trust Modes (configurable via `--bundle-trust-mode`)

**Out-of-band (OOB):** Operator copies the Cosign public key to the satellite separately. Every bundle is verified against this key. Strongest trust.

**Trust-on-first-use (TOFU):** First bundle includes the public key in `bundle.json.verification_key`. Satellite pins it. Subsequent bundles must verify against the pinned key. Weaker trust but zero extra setup.

#### Bundle Signing

- ECDSA P-256 key pair (same curve used in `internal/crypto/aes_provider.go`)
- Private key in GC environment; public key in `bundle_signing_keys` table
- Signing via `sigstore/cosign/v2` library
- `bundle.sig` is a Cosign-compatible detached signature

#### Key Rotation

1. GC generates new key pair, registers in `bundle_signing_keys`
2. GC produces a key rotation bundle signed with the OLD key, containing the new public key
3. Satellite verifies rotation bundle, adds new key to trust store
4. Transition period: both keys accepted
5. Old key revoked in DB

---

## Implementation Phases

### Phase 1: GC Bundle Generation

- DB migration (mode, bundles, signing_keys)
- Bundle generator package (pull images into OCI layout, tar.gz)
- Bundle signing (Cosign library)
- Storage abstraction (local disk + S3, presigned URLs)
- API endpoints (generate, list, download, applied)
- Signing key management APIs

### Phase 2: Satellite Bundle Import

- CLI flags (air-gapped, bundle-path, bundle-watch-dir, bundle-verify-key, trust-mode)
- Bundle verifier (Cosign signature verification)
- `LayoutReplicator` (implements `Replicator` from OCI layout)
- `BundleImportProcess` (scheduler.Process for watch mode)
- Air-gapped mode branching in `satellite.go`
- State persistence compatibility (write same PersistedState format)

### Phase 3: Differential Bundles

- Last-applied tracking in GC
- Artifact manifest diffing
- Delta OCI layout generation
- Removal handling on satellite side

### Phase 4: Reconciliation and Mode Switching

- Connected-to-airgapped and airgapped-to-connected transitions
- State continuity verification
- E2E test for full lifecycle

### Phase 5: Advanced Trust and Observability

- Key rotation protocol
- TOFU implementation
- SBOM generation (Syft) per image
- Per-image Cosign signatures in bundles
- Audit logging for bundle generation/application
- Dashboard integration for air-gapped satellite status

---

## Validation

Each phase should be validated with:

1. **Unit tests**: Bundle generation, signing/verification, OCI layout read/write, differential computation, presigned URL generation
2. **Integration test**: Generate bundle from GC, verify signature, import on satellite, confirm Zot serves images
3. **Reconciliation test**: Import bundle in air-gapped mode, switch to connected, verify state sync skips existing images and pulls only new ones
4. **Differential test**: Generate full bundle, apply, change group state, generate differential, verify only delta images included
5. **Trust test**: Attempt import with wrong key (expect rejection), rotate key (expect acceptance), TOFU pin (expect subsequent verification)
6. **E2E**: New `e2e-airgap` variant in Taskfile with Docker Compose stack covering the full lifecycle

---

## Key Files Reference

| Existing File | Relevance |
|---|---|
| `internal/state/replicator.go` | `Replicator` interface that `LayoutReplicator` implements |
| `internal/state/state_persistence.go` | `PersistedState`/`Entity` structs (the format contract) |
| `internal/state/state_process.go` | Orchestration (works unchanged with swapped replicator) |
| `internal/state/fetcher.go` | TAR extraction pattern already exists |
| `internal/satellite/satellite.go` | Entry point for air-gapped mode branching |
| `internal/crypto/aes_provider.go` | ECDSA P-256 pattern to reuse for signing |
| `ground-control/internal/utils/helper.go` | `AssembleGroupState()` for bundle artifact resolution |
| `ground-control/internal/server/satellite_handlers.go` | Satellite CRUD (add mode field) |
| `ground-control/internal/server/routes.go` | Route registration |
| `pkg/config/config.go` | Config structs (add air-gapped fields) |
| `cmd/main.go` | CLI flags |
| `go.mod` | Already has `go-containerregistry v0.20.3` (includes `v1/layout`) |

## More Information

- [Zarf GitHub](https://github.com/zarf-dev/zarf)
- [Zarf Differential Packages](https://docs.zarf.dev/tutorials/9-package-create-differential/)
- [Zarf Package Structure](https://docs.zarf.dev/ref/packages/)
- [Zarf SBOMs](https://docs.zarf.dev/ref/sboms)
- [OCI Image Layout Specification](https://github.com/opencontainers/image-spec/blob/main/image-layout.md)
- [ADR-0005: SPIFFE Identity and Security](0005-spiffe-identity-and-security.md) (trust chain foundation)
- [ADR-0001: Skopeo vs Crane](0001-skopeo-vs-crane.md) (chose go-containerregistry)
- [ADR-0002: Zot vs Docker Registry](0002-zot-vs-docker-registries.md) (chose Zot)
