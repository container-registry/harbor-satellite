# System Architecture Overview

This document provides a high-level overview of the Harbor Satellite architecture, its design principles, and deployment patterns. For detailed technical specifications, see the [Components Guide](components.md).

## System Components

The Harbor Satellite system consists of three main components:

1. **Ground Control** - Central management component
2. **Satellite** - Edge deployment component
3. **Registry** - Local container registry (Zot)

For detailed technical specifications of each component, see [Components Guide](components.md).

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

## Deployment Patterns

### Basic Edge Registry
```
[Central Harbor] <-> [Satellite] <-> [Local Workloads]
```
- Simple deployment
- Direct image serving
- Basic synchronization

### Planned Deployment Patterns

#### 1. Spegel Registry Pattern
```
[Central Harbor] <-> [Satellite] <-> [Spegel Nodes] <-> [Local Workloads]
```
- Peer-to-peer distribution
- Bandwidth optimization
- Cluster-wide caching

#### 2. Proxy Registry Pattern
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

## Related Documentation

1. [Components Guide](components.md) - Detailed components specifications
2. [Configuration Guide](../user-guide/configuration.md) - System configuration
3. [Use Cases Guide](use-cases.md) - Deployment patterns and examples