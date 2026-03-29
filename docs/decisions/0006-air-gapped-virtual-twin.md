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
┌─────────────────────────────────────────────────────────────────────────┐
│                          CLOUD / CONNECTED SIDE                        │
│                                                                        │
│  ┌──────────────┐       ┌──────────────────┐       ┌───────────────┐  │
│  │              │       │                  │       │               │  │
│  │    Harbor    │◄──────│  Ground Control  │──────►│  Bundle Store │  │
│  │   Registry   │ pull  │                  │ store │  (Disk / S3)  │  │
│  │              │ images│  ┌────────────┐  │       │               │  │
│  │  ┌────────┐  │       │  │  Virtual   │  │       └───────┬───────┘  │
│  │  │ Images │  │       │  │   Twins    │  │               │          │
│  │  │ State  │  │       │  │ (airgapped │  │          presigned       │
│  │  │Artifacts│  │       │  │ satellites)│  │           URLs           │
│  │  └────────┘  │       │  └────────────┘  │               │          │
│  └──────┬───────┘       └────────┬─────────┘               │          │
│         │                        │                          │          │
└─────────┼────────────────────────┼──────────────────────────┼──────────┘
          │                        │                          │
          │ OCI pull               │ state sync               │ download
          │ (live)                 │ (live)                   │ .tar.gz
          │                        │                          │
    ┌─────▼─────┐            ┌─────▼─────┐            ┌──────▼───────┐
    │ Connected │            │ Connected │            │   Operator   │
    │ Satellite │            │ Satellite │            │   Workstation│
    │   Site A  │            │   Site B  │            │              │
    │           │            │           │            └──────┬───────┘
    │ ┌───────┐ │            │ ┌───────┐ │                   │
    │ │  Zot  │ │            │ │  Zot  │ │          physical transport
    │ └───────┘ │            │ └───────┘ │           (USB / DVD / etc)
    └───────────┘            └───────────┘                   │
                                                             │
    ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─  AIR GAP  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┼ ─ ─ ─ ─
                                                             │
                                                      ┌──────▼───────┐
                                                      │  Air-Gapped  │
                                                      │  Satellite   │
                                                      │   Site C     │
                                                      │              │
                                                      │  ┌────────┐  │
                                                      │  │  Zot   │  │
                                                      │  └────────┘  │
                                                      │  ┌────────┐  │
                                                      │  │bundle  │  │
                                                      │  │watcher │  │
                                                      │  └────────┘  │
                                                      └──────────────┘
```

### Flow 1: Day-Zero Setup (Air-Gapped Satellite)

**Persona: Fleet Admin** — manages both connected and air-gapped sites from GC.
**Persona: Site Operator** — physically present at the air-gapped site.

```
  Fleet Admin                     Ground Control                 Site Operator
  (browser/CLI)                   (cloud)                        (air-gapped site)
       │                               │                               │
       │  1. Register satellite        │                               │
       │  POST /api/satellites         │                               │
       │  { name: "classified-01",     │                               │
       │    mode: "airgapped",         │                               │
       │    groups: ["mil-images"] }   │                               │
       ├──────────────────────────────►│                               │
       │                               │── creates virtual twin        │
       │                               │── creates robot account       │
       │                               │                               │
       │  2. Generate first bundle     │                               │
       │  POST /api/satellites/        │                               │
       │    classified-01/bundles      │                               │
       │  { type: "full" }             │                               │
       ├──────────────────────────────►│                               │
       │                               │── pull images from Harbor     │
       │                               │── build OCI layout            │
       │                               │── sign bundle.json            │
       │                               │── create .tar.gz              │
       │                               │── store + record              │
       │  ◄── presigned URL (30min) ───│                               │
       │                               │                               │
       │  3. Download bundle           │                               │
       │  GET /bundles/{id}/download   │                               │
       │  ?token=hmac&expires=...      │                               │
       ├──────────────────────────────►│                               │
       │  ◄── bundle.tar.gz stream ────│                               │
       │                               │                               │
       │  4. Export signing key        │                               │
       │  (for OOB trust mode)         │                               │
       │  GET /api/signing-keys        │                               │
       ├──────────────────────────────►│                               │
       │  ◄── public key PEM ──────────│                               │
       │                               │                               │
       │                                                               │
       │  5. Transfer to air-gapped site                               │
       │  ┌───────────────────────────────────────────────────────┐    │
       │  │  USB drive contains:                                  │    │
       │  │  - bundle.tar.gz                                      │    │
       │  │  - cosign-verify.pub  (OOB trust mode)                │    │
       │  │  - satellite binary   (first-time only)               │    │
       │  └───────────────────────────────────────────────────────┘    │
       │                                ···physical transport···       │
       │                                                               │
       │                                               6. Deploy       │
       │                                               ┌───────────────┤
       │                                               │ Install       │
       │                                               │ satellite     │
       │                                               │ binary        │
       │                                               │               │
       │                                               │ Place key at  │
       │                                               │ /etc/sat/     │
       │                                               │   cosign.pub  │
       │                                               │               │
       │                                               │ Place bundle  │
       │                                               │ at /bundles/  │
       │                                               ├───────────────┘
       │                                               │
       │                                               │  7. Start satellite
       │                                               │  $ satellite \
       │                                               │    --air-gapped \
       │                                               │    --bundle-watch-dir /bundles \
       │                                               │    --bundle-verify-key /etc/sat/cosign.pub
       │                                               │
       │                                               │── verify bundle.sig
       │                                               │── extract OCI layout
       │                                               │── import images to Zot
       │                                               │── write state.json
       │                                               │── Zot serves images
       │                                               │
       │                                               │  8. Workloads pull
       │                                               │  from local Zot
       │                                               │  (containerd/docker
       │                                               │   configured as mirror)
       ▼                                               ▼
```

### Flow 2: Ongoing Updates (Differential Bundle)

```
  Fleet Admin                     Ground Control                 Site Operator
       │                               │                               │
       │  1. Update group images       │                               │
       │  POST /api/groups/sync        │                               │
       │  { group: "mil-images",       │                               │
       │    artifacts: [...updated] }  │                               │
       ├──────────────────────────────►│                               │
       │                               │── update group state          │
       │                               │   artifact in Harbor          │
       │                               │                               │
       │  2. Generate diff bundle      │                               │
       │  POST /api/satellites/        │                               │
       │    classified-01/bundles      │                               │
       │  { type: "differential" }     │                               │
       ├──────────────────────────────►│                               │
       │                               │── load last applied bundle    │
       │                               │── diff: 2 new, 1 updated,    │
       │                               │         1 removed             │
       │                               │── pull only 3 images (not     │
       │                               │   the 20 unchanged ones)      │
       │                               │── sign + tar.gz               │
       │  ◄── presigned URL ───────────│                               │
       │                               │                               │
       │  3. Download + transport      │                               │
       │  (much smaller than full)     │                               │
       │  ─ ─ ─ ─ ─ ─ ─ physical ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─►│
       │                               │                               │
       │                               │               4. Drop bundle  │
       │                               │               in watch dir    │
       │                               │               $ cp bundle.tar.gz
       │                               │                  /bundles/    │
       │                               │                               │
       │                               │               5. Auto-import  │
       │                               │               BundleWatcher   │
       │                               │               detects file    │
       │                               │               ── verify sig   │
       │                               │               ── import 3 new │
       │                               │               ── delete 1     │
       │                               │               ── update state │
       │                               │               ── move to      │
       │                               │                  processed/   │
       │                               │                               │
       │  6. Mark applied (optional,   │                               │
       │     for tracking)             │                               │
       │  POST /api/satellites/        │                               │
       │    classified-01/bundles/     │                               │
       │    42/applied                 │                               │
       ├──────────────────────────────►│                               │
       │                               │── record applied_at           │
       │                               │── enables next differential   │
       ▼                               ▼                               ▼
```

### Flow 3: Reconciliation (Air-Gapped to Connected)

```
  Fleet Admin                     Ground Control           Satellite (was air-gapped)
       │                               │                               │
       │                               │                    ┌──────────┤
       │                               │                    │ Currently│
       │                               │                    │ running  │
       │                               │                    │ --air-   │
       │                               │                    │ gapped   │
       │                               │                    │          │
       │                               │                    │ Has 25   │
       │                               │                    │ images   │
       │                               │                    │ from last│
       │                               │                    │ bundle   │
       │                               │                    │          │
       │                               │                    │ state.json
       │                               │                    │ is up to │
       │                               │                    │ date     │
       │                               │                    └──────────┤
       │                               │                               │
       │  1. Switch mode in GC         │                               │
       │  PATCH /api/satellites/       │                               │
       │    classified-01              │                               │
       │  { mode: "connected" }        │                               │
       ├──────────────────────────────►│                               │
       │                               │── now expects heartbeats      │
       │                               │                               │
       │                               │              2. Reconfigure   │
       │                               │              satellite        │
       │                               │              $ satellite \    │
       │                               │                --token "abc" \│
       │                               │                --ground-control-url
       │                               │                  https://gc.. │
       │                               │                               │
       │                               │◄── 3. ZTR registration ──────┤
       │                               │──── credentials + state URL ─►│
       │                               │                               │
       │                               │         4. State replication  │
       │                               │         loads state.json      │
       │                               │         (from last bundle)    │
       │                               │                               │
       │                               │         compares persisted    │
       │                               │         entities vs remote:   │
       │                               │                               │
       │                               │         ┌─────────────────┐   │
       │                               │         │ 25 images match │   │
       │                               │         │  -> skip (have) │   │
       │                               │         │  3 new remote   │   │
       │                               │         │  -> pull         │   │
       │                               │         │  1 removed      │   │
       │                               │         │  -> delete       │   │
       │                               │         └─────────────────┘   │
       │                               │                               │
       │                               │◄── 5. Heartbeat reporting ───┤
       │                               │    (CPU, memory, images, etc) │
       │                               │                               │
       │                               │    6. Ongoing: satellite      │
       │                               │    polls normally, like       │
       │                               │    any connected satellite    │
       ▼                               ▼                               ▼
```

### Flow 4: Trust Verification

```
                    OOB Trust Mode                          TOFU Trust Mode
                    (out-of-band)                           (trust on first use)

    ┌─────────────────────────────┐        ┌─────────────────────────────┐
    │  BEFORE FIRST BUNDLE        │        │  FIRST BUNDLE               │
    │                             │        │                             │
    │  Admin exports public key   │        │  bundle.json contains:      │
    │  from GC and delivers it    │        │  { ...                      │
    │  separately to satellite    │        │    verification_key:        │
    │                             │        │    "-----BEGIN PUBLIC..."   │
    │  satellite --bundle-verify  │        │  }                          │
    │    -key /etc/sat/cosign.pub │        │                             │
    │                             │        │  Satellite has no key yet   │
    │  Key is pre-loaded before   │        │  -> accepts key from bundle │
    │  any bundle is ever opened  │        │  -> pins it locally         │
    │                             │        │  -> logs WARNING: TOFU pin  │
    └──────────────┬──────────────┘        └──────────────┬──────────────┘
                   │                                      │
                   ▼                                      ▼
    ┌─────────────────────────────┐        ┌─────────────────────────────┐
    │  EVERY BUNDLE               │        │  SUBSEQUENT BUNDLES         │
    │                             │        │                             │
    │  1. Read bundle.sig         │        │  1. Read bundle.sig         │
    │  2. Read bundle.json        │        │  2. Read bundle.json        │
    │  3. Verify sig against      │        │  3. Verify sig against      │
    │     pre-loaded public key   │        │     pinned public key       │
    │  4. Pass -> proceed         │        │  4. Pass -> proceed         │
    │     Fail -> reject          │        │     Fail -> reject          │
    │                             │        │                             │
    │  Trust: STRONG              │        │  Trust: MODERATE            │
    │  (key never came from       │        │  (first bundle unverified,  │
    │   an unverified source)     │        │   subsequent ones verified) │
    └─────────────────────────────┘        └─────────────────────────────┘
```

### Flow 5: Operator Experience — Side by Side

```
    ╔══════════════════════════════════╦══════════════════════════════════╗
    ║       CONNECTED SATELLITE       ║      AIR-GAPPED SATELLITE       ║
    ╠══════════════════════════════════╬══════════════════════════════════╣
    ║                                  ║                                  ║
    ║  DAY 0: REGISTER                ║  DAY 0: REGISTER                ║
    ║  ─────────────────              ║  ─────────────────              ║
    ║  POST /api/satellites           ║  POST /api/satellites           ║
    ║  { name: "site-a",             ║  { name: "site-c",             ║
    ║    mode: "connected",          ║    mode: "airgapped",          ║
    ║    groups: ["prod"] }          ║    groups: ["prod"] }          ║
    ║                                  ║                                  ║
    ║  Same API. Same groups.         ║  Same API. Same groups.         ║
    ║                                  ║                                  ║
    ╠══════════════════════════════════╬══════════════════════════════════╣
    ║                                  ║                                  ║
    ║  DAY 0: DEPLOY                  ║  DAY 0: DEPLOY                  ║
    ║  ────────────────               ║  ────────────────               ║
    ║  $ satellite \                  ║  Generate bundle:               ║
    ║    --token "abc" \             ║  POST .../bundles {type:"full"} ║
    ║    --ground-control-url \      ║  Download + transport           ║
    ║    https://gc.example.com      ║                                  ║
    ║                                  ║  $ satellite \                  ║
    ║  Auto: ZTR -> credentials      ║    --air-gapped \               ║
    ║  Auto: pull state -> replicate ║    --bundle-watch-dir /bundles\ ║
    ║  Auto: Zot serves images       ║    --bundle-verify-key cosign   ║
    ║                                  ║                                  ║
    ║                                  ║  Auto: verify -> import -> Zot ║
    ║                                  ║                                  ║
    ╠══════════════════════════════════╬══════════════════════════════════╣
    ║                                  ║                                  ║
    ║  ONGOING: UPDATES               ║  ONGOING: UPDATES               ║
    ║  ────────────────               ║  ────────────────               ║
    ║  Update group in GC             ║  Update group in GC             ║
    ║  (same API)                     ║  (same API)                     ║
    ║                                  ║                                  ║
    ║  Satellite auto-syncs.          ║  Generate diff bundle.          ║
    ║  Nothing to do.                 ║  Transport to site.             ║
    ║                                  ║  Drop in /bundles/ dir.         ║
    ║                                  ║  Satellite auto-imports.        ║
    ║                                  ║                                  ║
    ╠══════════════════════════════════╬══════════════════════════════════╣
    ║                                  ║                                  ║
    ║  LOCAL WORKLOADS                ║  LOCAL WORKLOADS                ║
    ║  ───────────────                ║  ───────────────                ║
    ║  containerd/docker pulls from  ║  containerd/docker pulls from  ║
    ║  local Zot (mirror mode)       ║  local Zot (mirror mode)       ║
    ║                                  ║                                  ║
    ║  Identical experience.          ║  Identical experience.          ║
    ║                                  ║                                  ║
    ╠══════════════════════════════════╬══════════════════════════════════╣
    ║                                  ║                                  ║
    ║  MONITORING                     ║  MONITORING                     ║
    ║  ──────────                     ║  ──────────                     ║
    ║  Heartbeats to GC.             ║  No heartbeats (offline).       ║
    ║  Dashboard shows status.       ║  Dashboard shows "air-gapped"   ║
    ║                                  ║  + last bundle applied.         ║
    ║                                  ║                                  ║
    ╚══════════════════════════════════╩══════════════════════════════════╝
```

### Data Flow: Bundle Generation (Ground Control)

```
    Ground Control                                        Harbor Registry
    ┌─────────────────────────────────────────┐          ┌──────────────┐
    │                                         │          │              │
    │  POST /api/satellites/{name}/bundles    │          │  ┌────────┐  │
    │                                         │          │  │ nginx  │  │
    │  1. Resolve desired state               │   crane  │  │ redis  │  │
    │     ┌──────────────────────────┐        │  .Pull() │  │ app-v2 │  │
    │     │ satellite -> groups      │        │◄────────►│  │  ...   │  │
    │     │ groups -> artifact list  │        │          │  └────────┘  │
    │     │ (reuse AssembleGroupState│        │          │              │
    │     └──────────┬───────────────┘        │          └──────────────┘
    │                │                        │
    │  2. If differential:                    │
    │     ┌──────────▼───────────────┐        │
    │     │ Load last applied bundle │        │
    │     │ Diff artifact manifests  │        │
    │     │ Keep only: new + changed │        │
    │     └──────────┬───────────────┘        │
    │                │                        │
    │  3. Build OCI Image Layout              │
    │     ┌──────────▼───────────────┐        │
    │     │ For each image:          │        │
    │     │   v1.Image -> layout.Wrt │        │
    │     │                          │        │
    │     │ oci/                     │        │
    │     │ ├── oci-layout           │        │
    │     │ ├── index.json           │        │
    │     │ └── blobs/sha256/        │        │
    │     │     ├── <shared layers>  │  <-- auto-dedup by digest
    │     │     ├── <config blobs>   │        │
    │     │     └── <manifests>      │        │
    │     └──────────┬───────────────┘        │
    │                │                        │
    │  4. Write state files                   │
    │     ┌──────────▼───────────────┐        │
    │     │ state/                   │        │
    │     │ ├── satellite-state.json │  <-- same format as
    │     │ ├── groups/              │      PersistedState
    │     │ │   └── group.json       │        │
    │     │ └── config.json          │        │
    │     └──────────┬───────────────┘        │
    │                │                        │
    │  5. Sign + Package                      │
    │     ┌──────────▼───────────────┐        │
    │     │ bundle.json --cosign-->  │        │
    │     │              bundle.sig  │        │
    │     │                          │        │
    │     │ tar czf bundle.tar.gz    │        │
    │     │   bundle.json            │        │
    │     │   bundle.sig             │        │
    │     │   oci/                   │        │
    │     │   state/                 │        │
    │     └──────────┬───────────────┘        │
    │                │                        │
    │  6. Store + Record                      │
    │     ┌──────────▼───────────────┐        │
    │     │ BundleStorage.Store()    │        │
    │     │ (disk or S3)             │        │
    │     │                          │        │
    │     │ INSERT INTO bundles (...) │        │
    │     │                          │        │
    │     │ Return presigned URL     │        │
    │     │ (30-min HMAC or S3)      │        │
    │     └──────────────────────────┘        │
    │                                         │
    └─────────────────────────────────────────┘
```

### Data Flow: Bundle Import (Satellite)

```
    Air-Gapped Satellite
    ┌────────────────────────────────────────────────────────────┐
    │                                                            │
    │  /bundles/bundle.tar.gz  <-- placed by operator           │
    │       │                                                    │
    │  1. BundleImportProcess detects new file                   │
    │       │                                                    │
    │  2. Extract to temp dir                                    │
    │       │                                                    │
    │       ▼                                                    │
    │  ┌──────────────────────────────────────┐                  │
    │  │ VERIFY                               │                  │
    │  │                                      │                  │
    │  │ bundle.json ---- cosign verify <---- cosign.pub         │
    │  │ bundle.sig  -----/                   (pre-loaded        │
    │  │                                       or TOFU-pinned)   │
    │  │                                      │                  │
    │  │ FAIL -> reject, log error, skip     │                  │
    │  │ PASS -> continue                    │                  │
    │  └──────────────┬───────────────────────┘                  │
    │                 │                                          │
    │  3. Parse state files                                      │
    │       │                                                    │
    │       ▼                                                    │
    │  ┌──────────────────────────────────────┐                  │
    │  │ state/satellite-state.json           │                  │
    │  │ state/groups/prod.json               │                  │
    │  │ state/config.json                    │                  │
    │  │                                      │                  │
    │  │ -> Parse into Entity[] list          │                  │
    │  │   (same structs as connected mode)   │                  │
    │  └──────────────┬───────────────────────┘                  │
    │                 │                                          │
    │  4. Import images                                          │
    │       │                                                    │
    │       ▼                                                    │
    │  ┌──────────────────────────────────────┐   ┌───────────┐ │
    │  │ LayoutReplicator                     │   │           │ │
    │  │                                      │   │    Zot    │ │
    │  │ For each entity:                     │   │  (local)  │ │
    │  │   img = layout.Image(digest)         │   │           │ │
    │  │   remote.Write(img, zot_ref) -----------►│  ┌─────┐  │ │
    │  │                                      │   │  │nginx│  │ │
    │  │ Same Replicator interface as         │   │  │redis│  │ │
    │  │ BasicReplicator -- just different    │   │  │app  │  │ │
    │  │ image source                         │   │  └─────┘  │ │
    │  └──────────────┬───────────────────────┘   └───────────┘ │
    │                 │                                          │
    │  5. Handle removals (differential only)                    │
    │       │                                                    │
    │       ▼                                                    │
    │  ┌──────────────────────────────────────┐                  │
    │  │ differential.removed:                │                  │
    │  │   ["library/redis:7.0"]              │                  │
    │  │                                      │                  │
    │  │ -> crane.Delete(zot/library/redis:7.0│                  │
    │  └──────────────┬───────────────────────┘                  │
    │                 │                                          │
    │  6. Persist state                                          │
    │       │                                                    │
    │       ▼                                                    │
    │  ┌──────────────────────────────────────┐                  │
    │  │ SaveState(state.json, entities,      │                  │
    │  │          configDigest)               │                  │
    │  │                                      │                  │
    │  │ SAME FORMAT as connected mode        │                  │
    │  │ <-- this is what makes               │                  │
    │  │     reconciliation seamless          │                  │
    │  └──────────────┬───────────────────────┘                  │
    │                 │                                          │
    │  7. Cleanup                                                │
    │     mv bundle.tar.gz -> /bundles/processed/                │
    │     rm -rf temp dir                                        │
    │                                                            │
    └────────────────────────────────────────────────────────────┘
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
    base_bundle_id    INT REFERENCES bundles(id),
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

```
bundle.tar.gz
├── bundle.json              # Manifest: metadata, artifact list, signing info
├── bundle.sig               # Cosign detached signature of bundle.json
├── oci/                     # OCI Image Layout (go-containerregistry v1/layout)
│   ├── oci-layout           # {"imageLayoutVersion": "1.0.0"}
│   ├── index.json           # OCI Image Index
│   └── blobs/sha256/        # Content-addressed blobs (layer dedup automatic)
├── state/                   # State files (same JSON format satellite uses)
│   ├── satellite-state.json # SatelliteStateArtifact
│   ├── groups/
│   │   └── <group>.json     # StateArtifact per group
│   └── config.json          # Satellite config
└── sbom/                    # (Phase 2) Per-image SBOMs via Syft
    └── <digest>.spdx.json
```

**Why OCI Image Layout:** `go-containerregistry/pkg/v1/layout` is already available (v0.20.3 in go.mod, just not imported yet). Layer deduplication is automatic via content-addressed blobs. This is exactly what Zarf uses.

#### bundle.json Schema

```json
{
  "version": 1,
  "satellite_name": "edge-site-01",
  "bundle_type": "full",
  "base_bundle_id": null,
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

Generation flow:

1. **Resolve desired state**: Fetch satellite's groups and config. Reuse `AssembleGroupState()` from `ground-control/internal/utils/helper.go`.
2. **Compute differential** (if applicable): Load last applied bundle's `artifact_manifest` from DB. Diff against current artifact list.
3. **Pull images into OCI layout**: `crane.Pull()` to get `v1.Image`, then `layout.Write()` into `oci/` directory. Blobs dedup automatically by digest.
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
