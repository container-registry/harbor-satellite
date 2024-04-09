# Proposal: `Harbor Satellite`

Authors: Vadim Bauer / [Vad1mo](https://github.com/Vad1mo), Csaba Almasi, Philip Laine, Roald Brunell / [OneFlyingBanana](https://github.com/OneFlyingBanana), (David Huseby, ...?)

<!--Discussion: `Link to discussion issue, if applicable`-->

## Abstract

Harbor Satellite aims to bring Harbor container registries to edge locations, ensuring consistent, available, and integrity-checked images for edge computing environments. This proposal outlines the development of a stateful, standalone satellite that can function as a primary registry for edge locations and as a fallback option if the central Harbor registry is unavailable.

## Background

In recent years, containers have extended beyond their traditional cloud environments, becoming increasingly prevalent in remote and edge computing contexts. These environments often lack reliable internet connectivity, posing significant challenges in managing and running containerized applications due to difficulties in fetching container images. To address this, the project aims to decentralize container registries, making them more accessible to edge devices. The need for a satellite that can operate independently, store images on disk, and run indefinitely with stored data is crucial for maintaining operations in areas with limited or no internet connectivity.

## Proposal

The proposed change is to develop "Harbor Satellite", an extension to the existing Harbor container registry. This extension will enable the operation of decentralized registries on edge devices.

Harbor Satellite will synchronize with the central Harbor registry, when Internet connectivity permits it, allowing it to receive and store images. This will ensure that even in environments with limited or unreliable internet connectivity, containerized applications can still fetch their required images from the local Harbor Satellite.

Harbor Satellite will also include a toolset enabling the monitoring and management of local decentralized registries.

## Non-Goals

T.B.D.

## Rationale

Deploying a complete Harbor instance on edge devices in poor/no coverage areas could prove problematic since :

- Harbor wasn't designed to run on edge devices.(e.g. Multiple processes, no unattended mode)
- Harbor could behave unexpectedly in poor/no connectivity environments.
- Managing hundreds or thousands of container registries is not operationally feasible with Harbor
- (What is the difference to a registry mirror?)

Harbor Satellite aims to be resilient, lightweight and will be able to keep functioning independently from Harbor instances.

## Compatibility

Compatibility with all container registries or edge devices can't be guaranteed.

## Implementation

Harbor Satellite will run in a single container and will be divided in the following components :

- **Satellite Core** : pulling/pushing images from/to Harbor (using go-libp2p?) and pulling/pushing images from/to the local registry (using Skopeo and/or Crane?).
- **Registry Proxy** : storing required OCI artifacts locally (using zotregistry or docker registry?).

![Harbor Satellite Diagram](../images/harbor-satellite-diagram.svg)

<p align="center"><em>Harbor Satellite Diagram</em></p>

## Open issues (if applicable)

Harbor Satellite aims to manage, coordinate and schedule containers using a Kubernetes cluster.

Harbor Satellite also aims to use and benefit from Spegel, a registry mirror designed to optimize the pulling of container images within a Kubernetes cluster.

## Use cases

![Use Case #1](../images/use-case-1.png)
![Use Case #2](../images/use-case-2.png)
![Use Case #3](../images/use-case-3.png)
