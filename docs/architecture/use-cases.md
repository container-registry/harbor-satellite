# Use Cases and Deployment Patterns

This document outlines the current use cases and deployment patterns for Harbor Satellite, providing practical examples and implementation guidance.

## Current Implementation

### Basic Edge Registry

#### Configuration
For detailed configuration information, see [Configuration Reference](../user-guide/configuration.md).

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
