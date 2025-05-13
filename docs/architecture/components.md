# System Components

This document provides detailed information about the components that make up the Harbor Satellite system.

## Ground Control

Ground Control is the central management component that orchestrates the Harbor Satellite system.

### Responsibilities

- Manages satellite configurations
- Controls artifact distribution
- Handles satellite registration
- Maintains desired state
- Provides API endpoints for management


### API Endpoints

#### Health and Status
- `GET /health` - Check service health status

#### Groups Management
- `POST /groups/sync` - Synchronize groups
- `GET /groups/list` - List all groups
- `GET /groups/{group}` - Get group details
- `POST /groups/satellite` - Add satellite to group
- `DELETE /groups/satellite` - Remove satellite from group

#### Satellite Management
- `POST /satellites/register` - Register a new satellite
- `GET /satellites/ztr/{token}` - Zero-touch registration endpoint
- `GET /satellites/list` - List all satellites
- `GET /satellites/{satellite}` - Get satellite details
- `DELETE /satellites/{satellite}` - Remove satellite

## Satellite

The Satellite component runs at edge locations and manages local container images.

### Responsibilities

- Acts as a local container registry using Zot
- Synchronizes with central Harbor
- Manages local container images
- Handles image distribution
- Maintains local state

### State Management

The Satellite maintains state in a JSON file containing:
- Artifact information
- Registry URLs
- Configuration settings

## Registry (Zot)

The Registry component uses Zot as the local container registry. For more information about the choice of Zot, see [Zot vs Docker Registries ADR](../decisions/0002-zot-vs-docker-registries.md).

### Responsibilities

- Storing container images locally
- Serving images to local workloads
- Managing image metadata
- Handling image operations

## Component Interactions

### Ground Control to Satellite

1. **Configuration Updates**
   - Ground Control pushes configuration changes
   - Satellite applies updates
   - State is synchronized

2. **State Synchronization**
   - Ground Control maintains desired state
   - Satellite reports current state
   - Differences are reconciled

3. **Health Monitoring**
   - Satellite reports health status
   - Ground Control tracks status
   - Alerts are generated if needed

### Satellite to Registry

1. **Image Management**
   - Satellite requests image operations
   - Registry performs operations
   - Results are reported back

2. **Metadata Management**
   - Registry maintains metadata
   - Satellite queries metadata
   - Updates are synchronized

### Satellite to Local Workloads

1. **Image Serving**
   - Workloads request images
   - Satellite serves from local registry
   - Pull requests are handled

2. **Health Reporting**
   - Workloads report health
   - Satellite aggregates reports
   - Status is forwarded to Ground Control
