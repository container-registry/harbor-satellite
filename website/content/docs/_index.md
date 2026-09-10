---
title: "Harbor Satellite"
weight: 1
---

Harbor Satellite extends [Harbor](https://goharbor.io) container registry to edge computing environments. It automatically synchronizes OCI content from your central Harbor instance into a persistent local OCI layout or an external registry, with zero-trust identity, fleet management, and automatic credential rotation.

## What is Harbor Satellite?

Harbor Satellite is a registry fleet management and artifact distribution system. It has three components:

- **Harbor** - Your central container registry in the cloud, holding all your images
- **Ground Control** - A management service that handles device onboarding, identity, and decides which images go to which edge locations
- **Satellite** - A lightweight binary that runs at each edge location and automatically copies the OCI content it needs into a local ORAS OCI layout or a BYO registry

Together, these components let you manage container images across hundreds of edge locations from a single control plane.

## Why Harbor Satellite?

Running containers at the edge creates three problems:

**Reliability** - Edge locations have unreliable network connections. Satellite prepositions content in persistent local storage and resumes reconciliation when connectivity returns. BYO registry mode or experimental k3s/RKE2 direct delivery can make that content available to workloads today; transparent proxy serving is tracked in ADR-0009.

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

### Satellite (Edge Store)

Runs at each edge location. A single binary that:

- Connects to a local SPIRE agent to get its identity (X.509 SVID - a cryptographic identity document)
- Registers with Ground Control over mTLS (mutual TLS - both sides verify each other's identity)
- Receives robot account credentials for pulling from Harbor
- Periodically fetches its desired state (which images to have)
- Replicates OCI content from Harbor into a local ORAS OCI layout by default
- Preserves remote-to-remote replication to an external registry in BYO mode
- Configures local container runtimes (containerd, Docker, CRI-O, Podman) to use itself as a mirror

## Next Steps

- [Overview](overview.md) - Deployment models, supported platforms, and how to choose the right setup
- [Installation](installation.md) - Install Ground Control and Satellite (binary, Docker Compose, Helm)
- [Architecture](architecture.md) - Understand the full flow of how these components work together
- [Quickstart](quickstart.md) - Get Harbor Satellite running end-to-end with SPIFFE
- [Certificate Issuer](certificate-issuer.md) - TLS/SPIFFE certificate chain and issuer hierarchy
