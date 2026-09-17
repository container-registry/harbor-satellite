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

- Maintains a local OCI image layout using ORAS
- Synchronizes with central Harbor
- Manages local OCI content
- Handles image distribution
- Can replicate to an external registry in BYO mode
- Maintains local state

### 3. Store

The Store abstraction is responsible for:

- Copying OCI content from its source
- Serving images to local workloads
- Managing image metadata
- Handling image operations
- Replicating images to an external registry in BYO mode
- Removing references and unreferenced local content

## System Architecture

### High-Level Architecture

```
[Central Harbor Registry]
         ↓
[Ground Control]
         ↓
[Satellite]
    ↓
[Local OCI Layout (ORAS)]
    ↓
[Local Workloads]
```

### Component Interactions

1. **Ground Control to Satellite**
   - Configuration updates
   - State synchronization
   - Health monitoring
   - Registration management

2. **Satellite to Store**
   - OCI graph copying
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

### Basic Edge Store
```
[Central Harbor] <-> [Satellite] <-> [Local OCI Layout] <-> [Local Workloads]
```
- Simple deployment
- Direct image serving
- Basic synchronization
- Uses ORAS OCI layout storage

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
[Central Harbor] → [Ground Control] → [Satellite] → [OCI Store]
```

### 2. State Synchronization
```
[Ground Control] ←→ [Satellite] → [OCI Store]
```

### 3. Health Monitoring
```
[Satellite] → [Ground Control]
```

## Next Steps

1. [Components Guide](components.md) - Detailed component documentation
2. [Use Cases Guide](use-cases.md) - Deployment patterns
3. [Configuration Guide](../user-guide/configuration.md) - System configuration
