# Basic Concepts

This guide explains the core concepts and terminology used in Harbor Satellite. Understanding these concepts will help you better utilize the system and make informed decisions about your deployment.

## Core Components

### Harbor Satellite
Harbor Satellite is the main component that runs at the edge location. It:
- Acts as a local container registry
- Synchronizes with the central Harbor registry
- Manages local container images
- Handles image distribution to local workloads

### Ground Control
Ground Control is the central management component that:
- Manages satellite fleets
- Controls artifact distribution
- Handles satellite registration and authentication
- Maintains the desired state for satellites

### Registry
The registry component is responsible for:
- Storing container images locally
- Serving images to local workloads
- Managing image metadata and layers

## Key Concepts

### Groups
Groups are logical collections of artifacts that should be distributed together. They:
- Define which container images should be available at edge locations
- Enable batch management of related artifacts
- Allow for organized distribution of images

### Artifacts
Artifacts are the container images and related resources that are distributed to satellites. They include:
- Container images
- Image tags
- Image digests
- Image metadata

### Satellite Registration
Satellite registration is the process of:
- Authenticating a satellite with Ground Control
- Assigning the satellite to specific groups
- Establishing secure communication channels

### State Synchronization
State synchronization ensures that:
- Local satellite state matches the desired state defined in Ground Control
- Container images are up to date
- Configuration changes are propagated correctly

## Architecture Patterns

### 1. Basic Edge Registry
```
[Central Harbor] <-> [Satellite] <-> [Local Workloads]
```
- Satellite acts as a local registry
- Synchronizes with central Harbor
- Serves images to local workloads

### 2. Spegel Registry Pattern
```
[Central Harbor] <-> [Satellite] <-> [Spegel Nodes] <-> [Local Workloads]
```
- Uses Spegel for efficient image distribution
- Enables peer-to-peer image sharing
- Optimizes bandwidth usage

### 3. Proxy Registry Pattern
```
[Central Harbor] <-> [Satellite (Proxy)] <-> [Local Workloads]
```
- Satellite acts as a proxy to central Harbor
- Forwards requests to central registry
- Useful when direct access is restricted


## Next Steps

1. [Configuration Guide](../user-guide/configuration.md) - Configure your deployment
2. [User Guide](../user-guide/README.md) - Learn how to use Harbor Satellite
3. [Architecture Guide](../architecture/overview.md) - Understand the system architecture 