# Harbor Satellite

Harbor Satellite extends [Harbor](https://goharbor.io) container registry to edge computing environments. It brings a lightweight, standalone OCI registry to each edge location that automatically syncs images from your central Harbor instance, with zero-trust identity, fleet management, and automatic credential rotation.

## What is Harbor Satellite?

Harbor Satellite is a registry fleet management and artifact distribution system. It has three components:

- **Harbor** - Your central container registry in the cloud, holding all your images
- **Ground Control** - A management service that handles device onboarding, identity, and decides which images go to which edge locations
- **Satellite** - A lightweight binary that runs at each edge location, embedding a local OCI registry ([Zot](https://zotregistry.dev)) and automatically pulling the images it needs

Together, these components let you manage container images across hundreds of edge locations from a single control plane.

## Why Harbor Satellite?

Running containers at the edge creates three problems:

**Reliability** - Edge locations have unreliable network connections. If your workloads pull images from a central registry and the network goes down, pods can't start. Satellite gives each location its own registry, so workloads always have local access to the images they need.

**Security** - Traditional approaches require shipping registry credentials to every edge device. Harbor Satellite uses [SPIFFE/SPIRE](https://spiffe.io) for zero-trust identity. After a one-time bootstrap, satellites get cryptographic identities from hardware-backed attestation. Registry credentials (Harbor robot accounts - service accounts with scoped pull permissions) are automatically created, delivered over mTLS (mutual TLS), and rotated by Ground Control.

**Fleet Management** - When you have dozens or hundreds of edge locations, manually managing which images go where becomes impossible. Ground Control lets you create groups of images and assign them to satellites. Change a group, and every satellite in that group automatically gets the update.

## Components

### Harbor (Central Registry)
Your existing Harbor instance in the cloud. Harbor Satellite does not replace Harbor - it extends it. All your images, projects, and access controls stay in Harbor.

### Ground Control (Management Plane)
Runs alongside Harbor in the cloud. Ground Control:
- Onboards satellites using SPIFFE/SPIRE identity
- Creates and rotates robot account credentials in Harbor on behalf of satellites
- Manages groups (collections of images) and assigns them to satellites
- Stores satellite state and config as OCI artifacts in Harbor
- Receives heartbeats and status reports from satellites

### Satellite (Edge Registry)
Runs at each edge location. A single binary that:
- Connects to a local SPIRE agent to get its identity (X.509 SVID - a cryptographic identity document)
- Registers with Ground Control over mTLS (mutual TLS - both sides verify each other's identity)
- Receives robot account credentials for pulling from Harbor
- Periodically fetches its desired state (which images to have)
- Replicates images from Harbor to its embedded Zot registry
- Configures local container runtimes (containerd, Docker, CRI-O, Podman) to use itself as a mirror

## Next Steps

- [Architecture](architecture.md) - Understand the full flow of how these components work together
- [Quickstart](quickstart.md) - Get Harbor Satellite running end-to-end
