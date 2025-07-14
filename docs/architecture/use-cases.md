# Use Cases and Deployment Patterns

This document outlines the current use cases and deployment patterns for Harbor Satellite, providing practical examples and implementation guidance.

## Current Implementation

### Basic Edge Registry

#### Configuration
```json
{
  "state_config": {
    "auth": {
      "name": "your_username",
      "registry": "https://harbor.example.com",
      "secret": "your_password"
    },
    "states": [
      "project1",
      "project2"
    ]
  },
  "environment_variables": {
    "ground_control_url": "http://localhost:8080",
    "log_level": "info",
    "use_unsecure": false,
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
      },
      {
        "name": "register_satellite",
        "schedule": "@every 00h00m05s"
      }
    ],
    "local_registry": {
      "url": "http://localhost:5000",
      "username": "admin",
      "password": "password",
      "bring_own_registry": false
    }
  }
}
```

#### Use Cases
- Remote deployments
- Development environments
- Testing environments
- Small-scale production
- Air-gapped environments
- Edge computing deployments

#### Implementation Details
- Single container deployment
- Uses Zot as the local registry
- Synchronizes with central Harbor registry
- Manages local image storage
- Handles state replication
- Configures container runtimes

## Planned Features

### 1. Spegel Registry Pattern
This feature is planned for future implementation to support:
- Large-scale deployments
- High-bandwidth environments
- Cluster deployments
- Multi-node setups

### 2. Proxy Registry Pattern
This feature is planned for future implementation to support:
- Network restrictions
- Security requirements
- Access control
- Bandwidth optimization

## Implementation Examples

### Basic Edge Setup

```bash
# 1. Deploy Satellite
docker run -d \
  --name satellite \
  -p 5000:5000 \
  -v /var/lib/registry:/var/lib/registry \
  harbor-satellite:latest

# 2. Configure Ground Control
curl -X POST http://localhost:8080/satellites/register \
  -H "Content-Type: application/json" \
  -d '{
    "name": "edge-satellite",
    "groups": ["edge-group"]
  }'

# 3. Deploy Workload
docker run -d \
  --name workload \
  localhost:5000/myapp:latest
```

## Next Steps

1. [Components Guide](components.md) - Detailed component documentation
2. [Configuration Guide](../user-guide/configuration.md) - System configuration