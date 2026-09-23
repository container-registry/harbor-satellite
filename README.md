# Harbor Satellite: Container Registries at the Edge

[![Go Report Card](https://goreportcard.com/badge/github.com/container-registry/harbor-satellite)](https://goreportcard.com/report/github.com/container-registry/harbor-satellite)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Harbor Satellite puts a lightweight, standalone OCI registry at every edge location and manages the whole fleet from one place. Each satellite pulls the images it is assigned from your central [Harbor](https://goharbor.io) registry and serves them locally, so workloads keep starting and updating when the uplink is slow, intermittent, or gone.

It is built for platform and engineering teams who run containers outside the data center and need a controlled, auditable way to get software there.

**Website and docs:** [satellite.container-registry.com](https://satellite.container-registry.com/)

## Is This For You?

Harbor Satellite fits if you operate containers in places like:

- Retail stores, branches, factories, or warehouses running a small cluster per site
- Ships, trains, vehicles, remote or mobile sites with intermittent connectivity
- Telco cell sites and far-edge compute with thousands of locations
- Air-gapped or highly isolated networks with restricted ingress/egress
- Multi-region setups that need images close to workloads (a container image CDN)

And you are running into problems like:

- Pods fail to start because the central registry is unreachable
- The same image is pulled over a thin WAN link once per node, per site
- There is no single view of which image versions are present at which site
- Running and upgrading a full registry at every location is not operationally feasible
- Edge devices need registry credentials without shipping long-lived secrets to them

## What You Get

| | |
|---|---|
| **Local availability** | An OCI registry at each site. Workloads pull from it, even with no upstream connection. |
| **Air-gapped operation** | Images are served from local storage. Replication resumes automatically when connectivity returns. |
| **Central fleet management** | Ground Control assigns image groups to satellites and tracks status (heartbeat, storage, cached images, sync duration) for every site. |
| **Desired-state sync** | Satellites reconcile against the state defined in Ground Control. You declare what should be at a site; the satellite makes it so. |
| **Zero-trust identity** | Optional SPIFFE/SPIRE mTLS with automatic certificate rotation. Harbor robot credentials are provisioned for you. |
| **Runtime integration** | Configures containerd, CRI-O, Podman, and Docker to use the local registry as a mirror, with upstream fallback. |
| **Lightweight** | Single binary with an embedded [Zot](https://zotregistry.dev) registry, unattended operation, builds for amd64, arm64, and many more architectures. |
| **Bring your own registry** | Replicate into an existing registry instead of the embedded Zot. |

## How It Works

![Basic Harbor Satellite Diagram](docs/images/harbor-satellite-overview.svg)

<p align="center"><em>Basic Harbor Satellite Diagram</em></p>

1. You define **groups** of artifacts (repository, tag, digest) and **configs** in Ground Control, then assign them to satellites.
2. A satellite at the edge **registers** with Ground Control, either with a one-time token or with a SPIFFE identity over mTLS (zero-touch registration).
3. The satellite periodically **fetches its desired state** and **replicates** the listed artifacts from Harbor into its local registry.
4. Local container runtimes **pull from the satellite**. If an image is missing locally, they fall back to the upstream registry when it is reachable.
5. The satellite **reports status** back to Ground Control, giving you a fleet-wide view of what is present where.

### Components

| Component | Runs in | Role |
|---|---|---|
| **Harbor** | Cloud | Central source of truth for all artifacts |
| **Ground Control** | Cloud | Fleet management: onboarding, grouping, desired state, credentials, status. Backed by PostgreSQL. |
| **Satellite** | Edge | Registers, syncs desired state, replicates artifacts, configures the container runtime |
| **Local registry** | Edge | Embedded Zot (default) or your own OCI registry |
| **SPIRE** (optional) | Cloud and edge | Issues X.509 identities for mTLS between satellites and Ground Control |

## Getting Started

Pick the path that matches where you are:

| Goal | Start here |
|---|---|
| Try it locally, dev or test | [Token-based quickstart](deploy/no-spiffe/quickstart.md) |
| Production with zero-trust identity | [SPIFFE/SPIRE quickstart](deploy/quickstart/README.md) |
| Compare auth methods | [QUICKSTART.md](QUICKSTART.md) |
| Install binaries, containers, Helm | [Installation docs](https://satellite.container-registry.com/docs/installation/) |
| Reference setup on k3s | [k3s reference architecture](docs/guides/k3s-reference-architecture.md) |

Prerequisite: a Harbor instance with the satellite adapter. See [harbor-next (satellite branch)](https://github.com/container-registry/harbor-next/tree/satellite).

### Deployment Choices

| Decision | Option | When |
|---|---|---|
| Authentication | Token-based ZTR | Dev, test, small deployments. No extra infrastructure. |
| | SPIFFE/SPIRE mTLS | Production and fleet scale. Supports join token, X.509 PoP, and SSH PoP attestation. |
| Registry | Embedded Zot (default) | Nothing else to run at the edge |
| | Bring your own | You already operate a registry at the site |

#### BYO (Bring Your Own) Registry

Pass the BYO flags via CLI or env vars:

| CLI Flag | Env Var | Description |
|---|---|---|
| `--byo-registry` | `BYO_REGISTRY` | Enable BYO mode |
| `--registry-url` | `REGISTRY_URL` | External registry URL (required if BYO) |
| `--registry-username` | `REGISTRY_USERNAME` | External registry username (optional) |
| `--registry-password` | `REGISTRY_PASSWORD` | External registry password (optional) |

A Docker Compose setup with `registry:2` as a sidecar is available:

```bash
task byo-up    # start satellite + registry:2
task byo-down  # stop and cleanup
```

### Container Runtime Configuration

The satellite can point local runtimes at its registry as a mirror, with fallback to upstream:

```bash
satellite --mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io
```

| Runtime | Config location | Notes |
|---|---|---|
| containerd | `/etc/containerd/config.toml` | Mirrors any registry |
| CRI-O | `/etc/crio/crio.conf.d/` | Mirrors any registry |
| Podman | `/etc/containers/registries.conf` | Mirrors any registry |
| Docker | `/etc/docker/daemon.json` | docker.io only, use `--mirrors=docker:true` |

Updating runtime config requires root. Docker needs a service restart to apply changes. Alternatives to mirroring are referencing the satellite registry directly, or rewriting image references with a Kubernetes mutating webhook.

## Distribution Patterns

### Pattern 1: Replicate from a remote registry to a local registry (implemented)

The satellite pulls the assigned images from Harbor and pushes them into the local OCI registry. Edge devices pull from the local registry; direct access to the remote registry remains possible when the network allows.

_Example: IoT devices at a site with limited or no connectivity need to run containerized workloads but cannot reliably reach central Harbor. The satellite serves all required images locally and refreshes them whenever the connection is back._

![Use Case #1](docs/images/satellite_use_case_1.svg)
<p align="center"><em>Use case #1</em></p>

### Pattern 2: Replicate to a local Spegel registry (in progress)

The satellite sends pull instructions to [Spegel](https://github.com/spegel-org/spegel) instances running on each node of a Kubernetes cluster. One node pulls from the remote registry and shares the image peer-to-peer with the other nodes, so each node does not pull individually. Network boundaries are the same as in Pattern 1.

_Example: a larger edge site where a single local registry cannot keep up with demand. Images are spread across nodes, and the satellite tells the cluster when, where, and what to pull._

![Use Case #2](docs/images/satellite_use_case_2.svg)
<p align="center"><em>Use case #2</em></p>

### Pattern 3: Proxy through the local registry (in progress)

The local registry runs in proxy mode. It pulls images from the remote registry on demand and caches them for local devices.

_Example: the central side cannot produce a list of images for a site ahead of time. The satellite forwards requests upstream and caches the results, so images stay available without a pre-compiled list._

![Use Case #3](docs/images/satellite_use_case_3.svg)
<p align="center"><em>Use case #3</em></p>

## Why Not Run Harbor at Every Site?

- Harbor is not designed for edge devices: multiple processes, a database, no unattended mode.
- Harbor can behave unpredictably with poor or no connectivity.
- Operating hundreds or thousands of Harbor instances is not feasible.
- A plain registry mirror does not give you central control over which artifacts are present where.

Harbor Satellite keeps Harbor as the central source of truth and puts only what is needed at the edge: a single process and a local registry that keep working independently of the central instance.

## Security

- Satellites authenticate to Ground Control with a single-use token or a SPIFFE X.509 SVID over mTLS.
- Ground Control provisions a Harbor robot account per satellite and issues fresh robot secrets on registration. No static registry credentials are shipped to the edge.
- Satellite config at rest can be encrypted with AES-256-GCM, bound to the device (`encrypt_config`).
- Hardware-backed identity via [CNCF PARSEC](https://parsec.community/) is available as an experimental, opt-in build (`--parsec-enabled`). See [ADR-0007](docs/decisions/0007-security-plugins-parsec.md).

Design details: [ADR-0004 Ground Control authentication](docs/decisions/0004-ground-control-authentication.md), [ADR-0005 SPIFFE identity and security](docs/decisions/0005-spiffe-identity-and-security.md).

## Status and Roadmap

Harbor Satellite is in active development. Implemented today: Ground Control, token and SPIFFE registration, desired-state replication (Pattern 1), status reporting, runtime mirror configuration, embedded Zot and BYO registries.

In progress:

- Spegel-based distribution (Pattern 2) and proxy mode (Pattern 3)

Planned:

- Downstream event executor: detect state changes at the site and trigger actions such as rollouts
- Broader hardware-backed identity support

Compatibility with every container registry and edge device cannot be guaranteed. If you are evaluating Harbor Satellite for your environment, [reach out](https://container-registry.com/contact/).

## Documentation

- [Architecture overview](docs/architecture/README.md), [components](docs/architecture/components.md), [use cases](docs/architecture/use-cases.md)
- [Architecture decision records](docs/decisions/)
- [Project website](https://satellite.container-registry.com/), source in [`website/`](website/README.md)

## Community, Discussion, Contribution, and Support

Harbor Satellite is part of the Harbor CNCF project.

- [#harbor-satellite on CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1) (request an invite at [slack.cncf.io](https://slack.cncf.io/))
- [GitHub issues](https://github.com/container-registry/harbor-satellite/issues)

## License

[Apache License 2.0](LICENSE)
