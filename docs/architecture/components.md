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

### Configuration

```json
{
  "satellite": {
    "name": "edge-registry",
    "group": "production",
    "registry": {
      "url": "http://localhost:5000",
      "auth": {
        "username": "admin",
        "password": "password"
      }
    }
  }
}
```

### API Endpoints

- `GET /api/satellites` - List satellites
- `POST /api/satellites` - Register satellite
- `GET /api/satellites/active` - List active satellites
- `GET /api/satellites/stale` - List stale satellites
- `GET /api/satellites/{satellite}` - Get satellite details
- `DELETE /api/satellites/{satellite}` - Remove satellite
- `GET /api/satellites/{satellite}/status` - Get satellite status
- `GET /api/satellites/{satellite}/images` - List cached satellite images

## Satellite

The Satellite component runs at edge locations and manages local container images.

### Responsibilities

- Maintains a local OCI image layout using ORAS
- Synchronizes with central Harbor
- Manages local container images
- Handles image distribution
- Maintains local state

### Configuration

```json
{
  "state": {
    "type": "file",
    "config": {
      "path": "/var/lib/harbor-satellite/state.json"
    }
  },
  "registry": {
    "url": "http://localhost:5000",
    "auth": {
      "username": "admin",
      "password": "password"
    }
  }
}
```

### State Management

The Satellite maintains state in a JSON file containing:
- Artifact information
- Registry URLs
- Configuration settings

## Store

The Store abstraction is responsible for copying and retaining OCI content.

### Responsibilities

- Storing OCI content locally in an OCI image layout
- Serving images to local workloads
- Copying images to an external registry in BYO mode
- Managing image metadata
- Removing references and garbage-collecting unreferenced local content

### Configuration

```json
{
  "app_config": {
    "bring_own_registry": false
  }
}
```

The local store path defaults to `<config-dir>/oci` and can be overridden with
`--registry-data-dir` or `REGISTRY_DATA_DIR`.

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

### Satellite to Store

1. **Content Management**
   - Satellite resolves desired OCI artifacts
   - The store copies complete descriptor graphs
   - Removed references are garbage-collected locally

2. **Metadata Management**
   - The OCI layout index maintains references
   - Satellite compares descriptor digests
   - Updates are synchronized

### Satellite to Local Workloads

1. **Image Serving**
   - Workloads request images
   - Satellite serves from local store 
   - Pull requests are handled

2. **Health Reporting**
   - Workloads report health
   - Satellite aggregates reports
   - Status is forwarded to Ground Control

## Security Considerations

### Authentication

- Token-based authentication
- Secure communication
- Certificate management

### Authorization

- Basic access control
- Resource permissions
- Group-based policies

### Network Security

- TLS encryption
- Network isolation
- Firewall rules

## Monitoring and Maintenance

### Metrics

- System performance
- Resource utilization
- Synchronization status
- Health indicators

### Logging

- Operation logs
- Error tracking
- Debug information

### Alerts

- Health alerts
- Performance alerts
- Security alerts

## Planned Features

### 1. Spegel Integration
- Peer-to-peer distribution
- Bandwidth optimization
- Cluster-wide caching

### 2. Proxy Mode
- Request forwarding
- Access control
- Network isolation

### 3. Enhanced Security
- Advanced authentication
- Role-based access control
- Audit logging
