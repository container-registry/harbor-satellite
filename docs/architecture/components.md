# System Components

This document provides detailed information about each component of the Harbor Satellite system, including their responsibilities, configurations, and interactions.

## Ground Control

### Overview
Ground Control is the central management component that orchestrates the entire Harbor Satellite system. It provides a unified interface for managing satellite fleets and controlling artifact distribution.

### Responsibilities
1. **Fleet Management**
   - Satellite registration and authentication
   - Group management
   - Configuration distribution
   - Health monitoring

2. **Artifact Control**
   - Image distribution management
   - Version control
   - Access control
   - State synchronization

3. **Monitoring and Reporting**
   - Health status tracking
   - Performance metrics
   - Error reporting
   - Audit logging

### Configuration
```json
{
  "server": {
    "port": 8080,
    "host": "0.0.0.0"
  },
  "database": {
    "host": "localhost",
    "port": 5432,
    "name": "groundcontrol",
    "user": "postgres",
    "password": "your_password"
  },
  "harbor": {
    "url": "https://harbor.example.com",
    "username": "admin",
    "password": "your_password"
  }
}
```

### API Endpoints
- `/health` - Health check endpoint
- `/groups/sync` - Group synchronization
- `/satellites/register` - Satellite registration
- `/satellites/status` - Satellite status
- `/artifacts/sync` - Artifact synchronization

## Satellite

### Overview
The Satellite component runs at edge locations, providing local container registry capabilities and managing synchronization with the central Harbor registry.

### Responsibilities
1. **Registry Management**
   - Local image storage
   - Image serving
   - Cache management
   - Layer optimization

2. **Synchronization**
   - State synchronization
   - Image replication
   - Configuration updates
   - Health reporting

3. **Local Operations**
   - Image pull handling
   - Request forwarding
   - Cache optimization
   - Resource management

### Configuration
```json
{
  "environment_variables": {
    "ground_control_url": "http://localhost:8080",
    "log_level": "info",
    "use_unsecure": true,
    "zot_config_path": "./registry/config.json",
    "token": "your_satellite_token",
    "jobs": [
      {
        "name": "replicate_state",
        "schedule": "@every 00h00m10s"
      },
      {
        "name": "update_config",
        "schedule": "@every 00h00m30s"
      }
    ]
  }
}
```

### Components
1. **Registry Manager**
   - Image storage
   - Layer management
   - Cache control

2. **Sync Manager**
   - State synchronization
   - Image replication
   - Configuration updates

3. **Health Monitor**
   - Status reporting
   - Resource monitoring
   - Error tracking

## Registry

### Overview
The Registry component is responsible for storing and serving container images locally at edge locations.

### Responsibilities
1. **Storage Management**
   - Image storage
   - Layer management
   - Cache control
   - Space management

2. **Image Serving**
   - Pull requests
   - Layer serving
   - Cache optimization
   - Bandwidth management

3. **Metadata Management**
   - Image metadata
   - Layer information
   - Cache status
   - Storage statistics

### Configuration
```json
{
  "storage": {
    "rootDirectory": "/var/lib/registry",
    "maxSize": "100GB",
    "cache": {
      "enabled": true,
      "maxSize": "10GB"
    }
  },
  "http": {
    "addr": ":5000",
    "tls": {
      "enabled": true,
      "cert": "/path/to/cert",
      "key": "/path/to/key"
    }
  }
}
```

### Features
1. **Storage Features**
   - Deduplication
   - Compression
   - Cache management
   - Space optimization

2. **Serving Features**
   - Layer streaming
   - Cache serving
   - Bandwidth control
   - Request optimization

3. **Management Features**
   - Garbage collection
   - Health checks
   - Statistics reporting
   - Maintenance tools

## Component Interactions

### 1. Ground Control to Satellite
```
[Ground Control] → [Satellite]
    ↓                ↓
[Configuration]  [Status]
    ↓                ↓
[Satellite] ← [Response]
```

### 2. Satellite to Registry
```
[Satellite] → [Registry]
    ↓            ↓
[Request]    [Storage]
    ↓            ↓
[Registry] ← [Response]
```

### 3. Registry to Local Workloads
```
[Registry] → [Local Workloads]
    ↓            ↓
[Image]      [Pull Request]
    ↓            ↓
[Registry] ← [Acknowledgment]
```

## Security Considerations

### 1. Authentication
- Token-based authentication
- Certificate validation
- Secure communication
- Access control

### 2. Authorization
- Role-based access
- Resource permissions
- Group policies
- Operation restrictions

### 3. Network Security
- TLS encryption
- Network isolation
- Firewall rules
- Access control

## Monitoring and Maintenance

### 1. Health Monitoring
- Component health
- Resource usage
- Performance metrics
- Error rates

### 2. Maintenance Procedures
- Regular updates
- Cache cleanup
- Storage optimization
- Configuration updates

### 3. Troubleshooting
- Log analysis
- Error tracking
- Performance tuning
- Resource optimization

## Next Steps

1. [Use Cases Guide](use-cases.md) - Common deployment patterns
2. [Configuration Guide](../user-guide/configuration.md) - System configuration
