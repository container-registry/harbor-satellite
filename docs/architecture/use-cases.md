# Use Cases and Deployment Patterns

This document outlines common use cases and deployment patterns for Harbor Satellite, providing practical examples and implementation guidance.

## Common Use Cases

### 1. Edge Computing Deployment

#### Scenario
Deploying containerized applications in remote locations with limited or intermittent network connectivity.

#### Architecture
```
[Central Harbor] <-> [Satellite] <-> [Edge Workloads]
```

#### Implementation
1. Deploy Satellite at edge location
2. Configure local registry
3. Set up synchronization
4. Deploy workloads

#### Benefits
- Local image availability
- Reduced network dependency
- Improved reliability
- Faster deployment

### 2. Air-Gapped Environment

#### Scenario
Running containerized applications in isolated networks with no direct internet access.

#### Architecture
```
[Central Harbor] → [Satellite] → [Air-Gapped Network]
```

#### Implementation
1. Deploy Satellite at network boundary
2. Configure secure transfer
3. Set up local registry
4. Deploy workloads

#### Benefits
- Secure deployment
- Controlled updates
- Network isolation
- Compliance support

### 3. Global Distribution

#### Scenario
Distributing container images across multiple geographical locations efficiently.

#### Architecture
```
[Central Harbor] <-> [Ground Control] <-> [Satellite Fleet]
    ↓                     ↓                     ↓
[Region 1]           [Region 2]           [Region 3]
```

#### Implementation
1. Deploy Ground Control
2. Set up regional satellites
3. Configure distribution
4. Manage updates

#### Benefits
- Efficient distribution
- Reduced bandwidth usage
- Improved availability
- Better scalability

### 4. High Availability Setup

#### Scenario
Ensuring container image availability for critical workloads.

#### Architecture
```
[Central Harbor] <-> [Satellite Cluster] <-> [Workloads]
    ↓                     ↓                     ↓
[Backup]            [Load Balancer]        [Failover]
```

#### Implementation
1. Deploy satellite cluster
2. Configure load balancing
3. Set up failover
4. Monitor health

#### Benefits
- Improved reliability
- Load distribution
- Automatic failover
- Better performance

## Deployment Patterns

### 1. Basic Edge Registry

#### Configuration
```json
{
  "satellite": {
    "mode": "edge",
    "sync_interval": "10m",
    "cache_size": "10GB"
  },
  "registry": {
    "type": "local",
    "storage": "/var/lib/registry"
  }
}
```

#### Use Cases
- Remote deployments
- Development environments
- Testing environments
- Small-scale production

### 2. Spegel Registry Pattern

#### Configuration
```json
{
  "satellite": {
    "mode": "spegel",
    "cluster_size": 3,
    "sync_strategy": "peer"
  },
  "registry": {
    "type": "distributed",
    "nodes": ["node1", "node2", "node3"]
  }
}
```

#### Use Cases
- Large-scale deployments
- High-bandwidth environments
- Cluster deployments
- Multi-node setups

### 3. Proxy Registry Pattern

#### Configuration
```json
{
  "satellite": {
    "mode": "proxy",
    "upstream": "https://harbor.example.com",
    "cache_enabled": true
  },
  "registry": {
    "type": "proxy",
    "cache_size": "5GB"
  }
}
```

#### Use Cases
- Network restrictions
- Security requirements
- Access control
- Bandwidth optimization

## Implementation Examples

### 1. Edge Computing Setup

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

### 2. Air-Gapped Deployment

```bash
# 1. Export images
docker save myapp:latest > myapp.tar

# 2. Transfer to air-gapped network
scp myapp.tar air-gapped-server:/tmp/

# 3. Import to Satellite
docker load < /tmp/myapp.tar
docker tag myapp:latest localhost:5000/myapp:latest
docker push localhost:5000/myapp:latest

# 4. Deploy workload
docker run -d \
  --name workload \
  localhost:5000/myapp:latest
```

### 3. Global Distribution Setup

```bash
# 1. Configure Ground Control
curl -X POST http://localhost:8080/groups/sync \
  -H "Content-Type: application/json" \
  -d '{
    "group": "global-group",
    "registry": "https://harbor.example.com",
    "artifacts": [
      {
        "repository": "myapp",
        "tag": ["latest"],
        "type": "docker"
      }
    ]
  }'

# 2. Register regional satellites
for region in "us-east" "eu-west" "ap-southeast"; do
  curl -X POST http://localhost:8080/satellites/register \
    -H "Content-Type: application/json" \
    -d "{
      \"name\": \"$region-satellite\",
      \"groups\": [\"global-group\"]
    }"
done
```

## Best Practices

### 1. Network Configuration
- Use appropriate network isolation
- Configure proper firewall rules
- Enable TLS encryption
- Implement access controls

### 2. Resource Management
- Monitor storage usage
- Configure appropriate cache sizes
- Implement cleanup policies
- Optimize bandwidth usage

### 3. Security
- Use secure authentication
- Implement proper authorization
- Regular security updates
- Monitor access logs

### 4. Monitoring
- Set up health checks
- Configure alerts
- Monitor performance
- Track resource usage

## Next Steps

1. [Configuration Guide](../user-guide/configuration.md) - System configuration
2. [Security Guide](../security/README.md) - Security best practices