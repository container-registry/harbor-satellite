# System Architecture Overview

This document provides a comprehensive overview of the Harbor Satellite architecture, including its components, interactions, and design principles.

## System Components

### 1. Ground Control

Ground Control is the central management component that orchestrates the Harbor Satellite system. It:

- Manages satellite configurations
- Controls artifact distribution
- Handles satellite registration
- Maintains desired state
- Provides API endpoints for management

### 2. Satellite

The Satellite component runs at edge locations and:

- Acts as a local container registry using Zot
- Synchronizes with central Harbor
- Manages local container images
- Handles image distribution
- Maintains local state

### 3. Registry

The Registry component (using Zot) is responsible for:

- Storing container images locally
- Serving images to local workloads
- Managing image metadata
- Handling image operations

## System Architecture

### High-Level Architecture

```
[Central Harbor Registry]
         ↓
[Ground Control]
         ↓
[Satellite]
    ↓
[Local Registry (Zot)]
    ↓
[Local Workloads]
```

### Component Interactions

1. **Ground Control to Satellite**
   - Configuration updates
   - State synchronization
   - Health monitoring
   - Registration management

2. **Satellite to Registry**
   - Image storage and retrieval
   - Metadata management
   - Layer management

3. **Satellite to Local Workloads**
   - Image serving
   - Pull request handling
   - Health reporting

## Design Principles

### 1. Decentralization
- Independent operation at edge
- Local image availability
- Reduced network dependency
- Improved resilience

### 2. Scalability
- Efficient resource utilization
- Optimized bandwidth usage
- Single-node architecture

### 3. Security
- Secure communication
- Authentication
- Token-based access
- Network isolation

### 4. Reliability
- State synchronization
- Health monitoring
- Automatic recovery
- Fault tolerance

## Deployment Pattern

### Basic Edge Registry
```
[Central Harbor] <-> [Satellite] <-> [Local Workloads]
```
- Simple deployment
- Direct image serving
- Basic synchronization
- Uses Zot as local registry

## Planned Deployment Patterns

### 1. Spegel Registry Pattern (Planned)
```
[Central Harbor] <-> [Satellite] <-> [Spegel Nodes] <-> [Local Workloads]
```
- Peer-to-peer distribution
- Bandwidth optimization
- Cluster-wide caching

### 2. Proxy Registry Pattern (Planned)
```
[Central Harbor] <-> [Satellite (Proxy)] <-> [Local Workloads]
```
- Request forwarding
- Access control
- Network isolation

## Data Flow

### 1. Image Distribution
```
[Central Harbor] → [Ground Control] → [Satellite] → [Local Registry]
```

### 2. State Synchronization
```
[Ground Control] ←→ [Satellite] ←→ [Local Registry]
```

### 3. Health Monitoring
```
[Satellite] → [Ground Control]
```

## Next Steps

1. [Components Guide](components.md) - Detailed component documentation
2. [Use Cases Guide](use-cases.md) - Deployment patterns
3. [Configuration Guide](../user-guide/configuration.md) - System configuration