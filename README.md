# Harbor Satellite — Software Distribution to the Edge

[![Go Report Card](https://goreportcard.com/badge/github.com/container-registry/harbor-satellite)](https://goreportcard.com/report/github.com/container-registry/harbor-satellite)

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


## Getting Started

### Prerequisites
- Go 1.21 or higher
- A running Harbor instance

### Installation
Clone the repository and build the binary:
```bash
git clone https://github.com/container-registry/harbor-satellite.git
cd harbor-satellite
go build -o harbor-satellite ./cmd/satellite
```

### Running the Satellite
Start the satellite by providing the configuration file:
```bash
./harbor-satellite --config config.yaml
```


## Background

Containers have extended beyond their traditional cloud environments, becoming increasingly prevalent in remote and edge computing contexts. These environments often lack reliable internet connectivity, posing significant challenges in managing and running containerized applications due to difficulties in fetching container images. To address this, the project aims to decentralize container registries, making them more accessible to edge devices. The need for a satellite that can operate independently, store images on disk, and run indefinitely with stored data.