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

- `GET /api/v1/satellites` - List satellites
- `POST /api/v1/satellites` - Register satellite
- `GET /api/v1/satellites/{name}` - Get satellite details
- `PUT /api/v1/satellites/{name}` - Update satellite configuration
- `DELETE /api/v1/satellites/{name}` - Remove satellite

## Satellite

The Satellite component runs at edge locations and manages local container images.

### Responsibilities

- Acts as a local container registry using Zot
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

## Registry

The Registry component (using Zot) is responsible for storing and serving container images.

### Responsibilities

- Storing container images locally
- Serving images to local workloads
- Managing image metadata
- Handling image operations

### Configuration

```json
{
  "storage": {
    "rootDirectory": "/var/lib/hotshot",
    "gc": true,
    "dedupe": true
  },
  "http": {
    "address": "0.0.0.0",
    "port": "5000"
  },
  "log": {
    "level": "debug"
  }
}
```

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
