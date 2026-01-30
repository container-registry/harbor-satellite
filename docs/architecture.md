# Architecture

This document provides a detailed overview of Harbor Satellite's architecture, including system components, data flow, and deployment patterns.

## System Overview

Harbor Satellite is a decentralized container registry solution that brings Harbor's power to edge computing environments. It consists of a central management component (Ground Control) and distributed edge registries (Satellites) that synchronize artifacts from a central Harbor instance.

## Core Components

### Ground Control

Ground Control is the central orchestration and management component that:

- **Device Management**: Registers and manages satellite instances
- **Configuration Management**: Stores and distributes satellite configurations
- **Artifact Orchestration**: Defines artifact groups and replication policies
- **State Management**: Tracks satellite status and health
- **API Gateway**: Provides REST API for management operations

**Technology Stack:**
- Go application server
- PostgreSQL database
- RESTful API with JWT authentication
- Docker container deployment

### Satellite

Satellite is the edge component that runs at remote locations:

- **Local Registry**: OCI-compliant registry using Zot
- **Artifact Synchronization**: Pulls and caches artifacts from central Harbor
- **State Management**: Maintains desired state and reports status
- **Container Runtime Integration**: Configures local CRI to use cached images
- **Health Monitoring**: Sends heartbeats and status updates

**Technology Stack:**
- Go application
- Zot OCI registry
- Cron-based scheduling
- Container runtime detection and configuration

### Harbor Registry

The central Harbor instance serves as the source of truth:

- **Artifact Storage**: Primary storage for container images
- **Access Control**: Robot accounts and permissions
- **Replication Management**: Satellite extension for edge coordination
- **Audit Logging**: Tracks artifact access and modifications

## Architecture Patterns

### Basic Edge Registry

```
[Central Harbor]
      ↓
[Ground Control] ←→ [Satellite]
      ↑                ↓
      └──────────── [Local Registry (Zot)]
                           ↓
                     [Edge Workloads]
```

**Use Case**: Single satellite serving local workloads with fallback to central registry.

**Characteristics**:
- Stateless satellite component
- Local Zot registry for caching
- Direct pull from central Harbor when needed
- Suitable for environments with intermittent connectivity

### Distributed Edge Cluster

```
[Central Harbor]
      ↓
[Ground Control] ←→ [Satellite 1] ←→ [Satellite 2] ←→ [Satellite 3]
      ↑                ↓                    ↓                    ↓
      └──────────── [Zot 1] ─────────── [Zot 2] ─────────── [Zot 3]
                           ↓                    ↓                    ↓
                     [Workload A]        [Workload B]        [Workload C]
```

**Use Case**: Multiple satellites in a cluster sharing artifacts via P2P replication.

**Characteristics**:
- Spegel integration for cluster-wide replication
- Reduced bandwidth usage through local sharing
- High availability across multiple nodes
- Kubernetes-native deployment

### Proxy Mode

```
[Central Harbor]
      ↓
[Ground Control] ←→ [Satellite (Proxy)]
      ↑                ↓
      └──────────── [Zot (Proxy Mode)]
                           ↓
                     [Edge Workloads]
```

**Use Case**: Satellite acts as a proxy when central registry is unreachable.

**Characteristics**:
- Registry runs in proxy mode
- Forwards requests to central Harbor
- Caches responses locally
- Transparent to edge workloads

## Data Flow

### Artifact Distribution

1. **Definition**: Artifacts are defined in Ground Control as groups with repositories, tags, and digests
2. **Assignment**: Groups are assigned to satellites during registration
3. **Replication**: Satellites periodically check for updates and pull new artifacts
4. **Caching**: Artifacts are stored locally in Zot registry
5. **Access**: Edge workloads pull from local registry with fallback to central

### State Management

1. **Registration**: Satellite registers with Ground Control and receives configuration
2. **Heartbeat**: Satellite sends periodic status updates
3. **State Sync**: Satellite pulls latest artifact lists and configurations
4. **Reporting**: Satellite reports replication status and health metrics

### Authentication Flow

1. **Admin Login**: Administrators authenticate with Ground Control
2. **Token Generation**: JWT tokens are issued for API access
3. **Satellite Auth**: Satellites use dedicated tokens for authentication
4. **Registry Access**: Robot accounts provide Harbor access

## Network Architecture

### Connectivity Patterns

- **Always Connected**: Full synchronization with central Harbor
- **Intermittent**: Local operation with periodic sync
- **Air-Gapped**: Completely disconnected operation
- **Hybrid**: Mix of connected and disconnected satellites

### Security Boundaries

- **Control Plane**: Ground Control API (9090)
- **Data Plane**: Satellite registries (8585)
- **Management**: Admin access to Ground Control
- **Edge Access**: Workload access to local registries

## Storage Architecture

### Ground Control Storage

- **PostgreSQL Database**:
  - User accounts and authentication
  - Satellite registrations and configurations
  - Group definitions and artifact metadata
  - Audit logs and status history

### Satellite Storage

- **Zot Registry Storage**:
  - OCI artifacts and manifests
  - Layer caching and deduplication
  - Metadata and indexes

- **Configuration Storage**:
  - Local configuration files
  - State files and caches
  - Log files and metrics

## Deployment Topologies

### Single Site

```
┌─────────────────┐
│ Ground Control  │
│                 │
│ ┌─────────────┐ │
│ │ PostgreSQL  │ │
└─┴─────────────┴─┘
         │
         │
┌─────────────────┐
│   Satellite     │
│                 │
│ ┌─────────────┐ │
│ │     Zot     │ │
│ │   Registry  │ │
└─┴─────────────┴─┘
```

### Multi-Site with Central Harbor

```
┌─────────────────┐    ┌─────────────────┐
│   Harbor        │    │ Ground Control  │
│   Registry      │    │                 │
│                 │    │ ┌─────────────┐ │
│ ┌─────────────┐ │    │ │ PostgreSQL  │ │
│ │  Database   │ │    └─┴─────────────┴─┘
└─┴─────────────┴─┘
         │                     │
         └─────────┬───────────┘
                   │
         ┌─────────┴─────────┐
         │                   │
┌─────────────────┐ ┌─────────────────┐
│   Satellite 1   │ │   Satellite 2   │
│                 │ │                 │
│ ┌─────────────┐ │ │ ┌─────────────┐ │
│ │     Zot     │ │ │ │     Zot     │ │
└─┴─────────────┴─┘ └─┴─────────────┴─┘
```

### Edge Cluster

```
┌─────────────────┐
│ Ground Control  │
└─────────────────┘
         │
         │
    ┌────┴────┐
    │         │
┌─────────────────┐ ┌─────────────────┐
│ Satellite +     │ │ Satellite +     │
│ Spegel          │ │ Spegel          │
│                 │ │                 │
│ ┌─────────────┐ │ │ ┌─────────────┐ │
│ │     Zot     │ │ │ │     Zot     │ │
└─┴─────────────┴─┘ └─┴─────────────┴─┘
    │         │
    └────┬────┘
         │
         │
    ┌────┴────┐
    │         │
┌─────────────────┐ ┌─────────────────┐
│   Workload      │ │   Workload      │
└─────────────────┘ └─────────────────┘
```

## Component Interactions

### Ground Control ↔ Satellite

- **Registration**: Satellite registers and receives token
- **Configuration**: Ground Control pushes configuration updates
- **Status**: Satellite reports health and replication status
- **Commands**: Ground Control can trigger satellite actions

### Satellite ↔ Harbor

- **Authentication**: Robot account credentials
- **Artifact Pull**: Satellite pulls artifacts from Harbor
- **Metadata Sync**: Synchronizes artifact lists and digests
- **Status Updates**: Reports replication progress

### Satellite ↔ Local Registry

- **Artifact Push**: Satellite pushes downloaded artifacts to Zot
- **Configuration**: Satellite configures Zot registry settings
- **Health Checks**: Monitors registry availability
- **Cleanup**: Manages storage and removes old artifacts

### Satellite ↔ Container Runtime

- **Mirror Configuration**: Sets up registry mirrors
- **Authentication**: Configures registry credentials
- **Fallback Logic**: Ensures fallback to central registry
- **Performance**: Optimizes pull performance

## Failure Modes and Recovery

### Network Partition

- **Detection**: Satellite detects connectivity loss
- **Operation**: Continues with local registry
- **Recovery**: Resumes sync when connectivity returns
- **Conflict Resolution**: Handles version conflicts

### Component Failure

- **Ground Control Down**: Satellites operate autonomously
- **Satellite Down**: Ground Control marks as stale
- **Registry Failure**: Satellite attempts restart
- **Storage Full**: Implements cleanup policies

### Data Consistency

- **Artifact Verification**: SHA256 digest validation
- **State Reconciliation**: Compares desired vs actual state
- **Conflict Resolution**: Last-write-wins strategy
- **Audit Trail**: Maintains operation logs

## Scalability Considerations

### Horizontal Scaling

- **Multiple Ground Control**: Load balancing with shared database
- **Satellite Clusters**: P2P replication reduces central load
- **Registry Sharding**: Distribute artifacts across multiple registries

### Performance Optimization

- **Caching**: Local artifact caching reduces network traffic
- **Deduplication**: Shared layers reduce storage requirements
- **Parallel Pulls**: Concurrent artifact downloads
- **Compression**: Efficient data transfer

### Monitoring and Observability

- **Metrics Collection**: CPU, memory, storage, and network metrics
- **Health Checks**: Component availability monitoring
- **Logging**: Structured logging with correlation IDs
- **Tracing**: Request tracing across components</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/architecture.md