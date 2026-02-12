# Harbor Satellite — Software Distribution to the Edge

Harbor Satellite brings the power of the Harbor container registry to edge computing. Satellite is a registry fleet management and artifact distribution solution around a central source of truth Harbor cluster.  

A lightweight, standalone registry at edge locations is acting as both a primary registry for local workloads and a fallback for the central Harbor instance. This stateful satellite registry ensures consistent, available, and integrity-checked container images for edge devices, even when network connectivity is intermittent or unavailable (air-gapped). Harbor Satellite optimizes image distribution and management for edge environments, addressing challenges like bandwidth limitations, remote fleet orchestration and artifact distribution.


## What Problems Harbor Satellite Addresses

- Fleet management for edge registries 
- Manage centrally the distribution artifacts to thousands of sites
- Predictable behavior in challenging connectivity situations
- Control edge artifact replication and presence
- Optimized resource and bandwidth utilization
- Transparent deployment process
- Air-gapped capable

## Use Cases

- Edge/IoT
- Environment without permanent connectivity
  - e.g. Remote Locations, Ships, Satellites, Firewalls
- High Availability of container images close to workloads
- Multiple sites
- Highly isolated sites 
- Restricted ingress/egress data flow
- Global image distribution 
- Container Image CDN
- Immediate application updates across regions


## How Satellite Works

- Satellite acts as gateway or proxy for containerized artifacts on site
- Artifacts are managed and orchestrated remotely
- Desired artifacts are securely synced with desired state on remote site 
- Satellite can initiate updates or deployments on site (Hooks)
- Runs on edge location as a single process in unattended mode


## Satellite Components

- Cloud Side
  - Ground Control
    -  Devices management
    -  Onboarding and grouping of sites
    -  State management and verification
  - Harbor (with satellite extension)
- Edge (Satellite Side)
  - Satellite
    - Artifact store and synchronizer
  - Runtime configuration updater
  - Downstream event executor


## Background

Containers have extended beyond their traditional cloud environments, becoming increasingly prevalent in remote and edge computing contexts. These environments often lack reliable internet connectivity, posing significant challenges in managing and running containerized applications due to difficulties in fetching container images. To address this, the project aims to decentralize container registries, making them more accessible to edge devices. The need for a satellite that can operate independently, store images on disk, and run indefinitely with stored data is crucial for maintaining operations in areas with limited or no internet connectivity.

## Concept

Harbor Satellite is an extension to the existing Harbor container registry that enables the operation of decentralized registries in edge locations.

Harbor Satellite will synchronize with the central Harbor registry, when Internet connectivity permits it, allowing it to receive and store images. This will ensure that even in environments with limited or unreliable internet connectivity, containerized applications can still fetch their required images from the local Harbor Satellite.

Harbor Satellite will also include a toolset enabling the monitoring and management of local decentralized registries.

## Documentation

Comprehensive documentation is available in the [`docs/`](docs/) directory:

- **[Getting Started](docs/getting-started.md)** - Complete setup guide for Ground Control and Satellite
- **[Configuration Reference](docs/configuration.md)** - All configuration options and examples
- **[API Reference](docs/api-reference.md)** - Ground Control REST API documentation
- **[Architecture](docs/architecture.md)** - System architecture and design
- **[Troubleshooting](docs/troubleshooting.md)** - Common issues and solutions
- **[Deployment Guides](docs/deployment/)** - Docker and Kubernetes deployment

## QuickStart

For rapid local development and evaluation, follow the step-by-step setup guide in [QUICKSTART.md](QUICKSTART.md). This covers satellite registration, Ground Control setup, and basic image replication.

## BYO (Bring Your Own) Registry

Satellite embeds a Zot registry by default. To use an external registry instead (e.g. `registry:2`), pass the BYO flags via CLI or env vars:

| CLI Flag | Env Var | Description |
|---|---|---|
| `--byo-registry` | `BYO_REGISTRY` | Enable BYO mode |
| `--registry-url` | `REGISTRY_URL` | External registry URL (required if BYO) |
| `--registry-username` | `REGISTRY_USERNAME` | External registry username (optional) |
| `--registry-password` | `REGISTRY_PASSWORD` | External registry password (optional) |

A docker-compose setup with `registry:2` as a sidecar is available:

```bash
task byo-up    # start satellite + registry:2
task byo-down  # stop and cleanup
```

## Non-Goals

T.B.D.

## Rationale

Deploying a complete Harbor instance on edge devices in poor/no coverage areas could prove problematic since:

- Harbor wasn't designed to run on edge devices (e.g. Multiple processes, no unattended mode).
- Harbor could behave unexpectedly in poor/no connectivity environments.
- Managing hundreds or thousands of container registries is not operationally feasible with Harbor.
- Harbor would be too similar to a simple registry mirror.

Harbor Satellite aims to be resilient, lightweight and will be able to keep functioning independently of Harbor instances.

## Compatibility

Compatibility with all container registries or edge devices can't be guaranteed.

## Implementation

### Overall Architecture

Harbor Satellite, at its most basic, will run in a single container and will be divided in the following 2 components:

- **Satellite**: Is responsible for moving artifacts from upstream (using Skopeo/Crane/Other), identifying the source, and reading the list of images that need to be replicated. Additionally, it can modify and manage container runtime configuration to prevent unnecessary remote fetches.
- **OCI Registry**: Is responsible for storing required OCI artifacts locally (using zotregistry or docker registry).
- **Ground Control**: Is a component of Harbor and is responsible for serving a Harbor Satellite with the list of images it needs.

![Basic Harbor Satellite Diagram](docs/images/harbor-satellite-overview.svg)

<p align="center"><em>Basic Harbor Satellite Diagram</em></p>

### Specific Use Cases

Harbor Satellite may be implemented following 1 or several of 3 different architectures depending on its use cases:

#### Use Case #1: Replicating from a remote registry to a local registry

In this basic use case, the stateless Satellite component pulls container images from a remote registry and pushes them to the local OCI-compliant registry. This local registry is then accessible to other local edge devices, which can pull the required images directly from it. _Direct access from edge devices to the remote registry is still possible when network conditions permit._ The Satellite component may also handle updating container runtime configurations and fetching image lists from Ground Control, a part of Harbor. The stateful local registry will also need to handle storing and managing data from local volumes. A typical scenario might look like this:

_In an edge computing environment where IoT devices are deployed to a location with limited or no internet connectivity, these devices need to run containerized images but cannot pull from a central Harbor registry. A local Harbor Satellite instance can be deployed and take up this role while Internet connectivity is unreliable and distribute all required images. Once a reliable connection is re-established, the Harbor Satellite instance will be able to pull required images from its central Harbor registry and thus store up-to-date images locally._

![Use Case #1](docs/images/satellite_use_case_1.svg)
<p align="center"><em>Use case #1</em></p>

#### Use Case #2: Replicating from a remote registry to a local Spegel registry

The stateless Satellite component sends pull instructions to Spegel instances running with each node of a Kubernetes cluster. The node will then directly pull images from a remote registry and share it with other local nodes, removing the need for each of them to individually pull an image from a remote registry.
The network interfaces (boundaries) represented in this use case should and will be the same as those represented in [Use Case #1](#use-case-1-replicating-from-a-remote-registry-to-a-local-registry). A typical use case would work as follows:

_In a larger scale edge computing environment with a significant amount of IoT devices needing to run containerized applications, a single local registry in might not be able to handle the increased amount of demands from edge devices. The solution is to deploy several registries to several nodes who are able to automatically replicate images across each other thanks to Spegel instances running together with each node. The Satellite component will use the same interface to instruct each node when, where and how to pull new images that need to be replicated across the cluster._

![Use Case #2](docs/images/satellite_use_case_2.svg)
<p align="center"><em>Use case #2</em></p>

#### Use Case #3: Proxying from a remote registry over the local registry

The stateless satellite component will be in charge of configuring the local OCI compliant registry, which will be running in proxy mode only. This local registry will then handle pulling necessary images from the remote registry and serving them up for use by local edge devices.
A typical use case would work as follows:

_When, for a number of possible different reasons, the remote registry side of the diagram would not be able to produce a list of images to push down to the Harbor Satellite, the Satellite would then act as a proxy and forward all requests from edge devices to the remote registry. This ensures the availability of necessary images without the need for a pre-compiled list of images_

![Use Case #3](docs/images/satellite_use_case_3.svg)
<p align="center"><em>Use case #3</em></p>

### Container Runtime Configuration

In each of these use cases, we need to ensure that IoT edge devices needing to run containers will be able to access the registry and pull images from it. To solve this issue, we propose 4 solutions:

1. By using **containerd** or **CRI-O** and  configuring a mirror within them.
2. By setting up an **HTTP Proxy** to manage and optimize pull requests to the registry.
3. By **directly referencing** the registry.
4. By **directly referencing** the registry and using Kubernetes' mutating webhooks to point to the correct registry.

## Development

The project is currently in active development. If you are interested in participating or using the product, [reach out](https://container-registry.com/contact/).

## Community, Discussion, Contribution, and Support

You can reach the Harbor community and developers via the following channels:

- [#harbor-satellite on CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1) (Request an invite to join the CNCF Slack via [slack.cncf.io](https://slack.cncf.io/))
