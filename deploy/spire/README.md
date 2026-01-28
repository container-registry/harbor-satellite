# SPIRE Deployment for Harbor Satellite

This directory contains configuration files for deploying SPIRE alongside Harbor Satellite for SPIFFE-based authentication.

## Quick Start (Development)

### 1. Generate CA Certificates

```bash
./generate-certs.sh
```

### 2. Create Docker Network

```bash
docker network create harbor-satellite
```

### 3. Start SPIRE Services

```bash
docker compose up -d
```

### 4. Register Ground Control

```bash
# Create registration entry for Ground Control
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -spiffeID spiffe://harbor-satellite.local/gc/main \
    -parentID spiffe://harbor-satellite.local/spire-agent \
    -selector unix:uid:0
```

### 5. Register a Satellite

```bash
# Generate join token for a new satellite
docker exec spire-server /opt/spire/bin/spire-server token generate \
    -spiffeID spiffe://harbor-satellite.local/satellite/region/default/my-satellite

# Create registration entry (after agent joins)
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -spiffeID spiffe://harbor-satellite.local/satellite/region/default/my-satellite \
    -parentID spiffe://harbor-satellite.local/spire-agent \
    -selector unix:uid:1000
```

## Configuration Files

- `server.conf` - SPIRE Server configuration
- `agent.conf` - SPIRE Agent configuration
- `docker-compose.yml` - Docker Compose for running SPIRE
- `generate-certs.sh` - Script to generate CA certificates

## Trust Domain

The default trust domain is `harbor-satellite.local`. Change this in production to match your organization's domain.

## SPIFFE ID Structure

```
spiffe://harbor-satellite.local/gc/main                    # Ground Control
spiffe://harbor-satellite.local/satellite/region/<r>/<n>   # Satellites
```

## Attestation Methods

### Join Token (Bootstrap)

Use for initial satellite bootstrapping:

```bash
# Generate token
docker exec spire-server /opt/spire/bin/spire-server token generate \
    -spiffeID spiffe://harbor-satellite.local/satellite/region/us-west/edge-1

# Use token to start agent
spire-agent run -config agent.conf -joinToken <token>
```

### X.509 PoP (Lazy Mode)

For development/testing, a shared certificate can be used for all satellites:

```bash
# Copy satellite-fleet.crt and satellite-fleet.key to agent
# Agent will use these for attestation
```

## Volumes

- `spire-server-data` - SPIRE Server data (CA keys, registration entries)
- `spire-server-socket` - SPIRE Server API socket
- `spire-agent-data` - SPIRE Agent data
- `spire-agent-socket` - Workload API socket (mount this in Ground Control/Satellite)

## Integration with Ground Control

To use SPIFFE authentication with Ground Control:

1. Mount the agent socket volume:
   ```yaml
   volumes:
     - spire-agent-socket:/run/spire/sockets
   ```

2. Set environment variables:
   ```yaml
   environment:
     SPIFFE_ENABLED: "true"
     SPIFFE_TRUST_DOMAIN: harbor-satellite.local
     SPIFFE_ENDPOINT_SOCKET: unix:///run/spire/sockets/agent.sock
   ```

## Production Considerations

1. Use persistent volumes for SPIRE data
2. Use a production-grade key manager (not disk-based)
3. Use proper node attestation (TPM, cloud provider)
4. Enable TLS for SPIRE server registration API
5. Configure proper CA rotation policies
6. Use separate trust domains for different environments
