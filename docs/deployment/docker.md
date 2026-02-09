# Docker Deployment

Deploy Harbor Satellite using Docker and Docker Compose. This guide demonstrates a Docker-based deployment for development and evaluation purposes.

## Overview

A typical Docker deployment includes:

- **Ground Control** — Central management service
- **PostgreSQL** — State database for Ground Control
- **Satellite** — Edge registry instance (one per location)
- **Zot** — Local image registry (embedded in Satellite)

## Prerequisites

- Docker Engine 20.10+ installed
- Docker Compose 2.0+
- 4GB RAM and 50GB disk (per machine)
- Network connectivity between Ground Control and Satellites
- Harbor instance with satellite support

## Directory Structure

Set up your deployment directory:

```bash
mkdir -p harbor-satellite-deployment
cd harbor-satellite-deployment

# Create subdirectories
mkdir -p {data,configs,scripts}
```

## Part 1: Ground Control Deployment

### Step 1: Create Environment File

```bash
cat > configs/groundcontrol.env << 'EOF'
# Server
GC_LISTEN_ADDR=0.0.0.0:8080
GC_LOG_LEVEL=info
GC_METRICS_PORT=8090
GC_READ_TIMEOUT=30s
GC_WRITE_TIMEOUT=30s

# Database
DB_HOST=postgres
DB_PORT=5432
DB_USER=groundcontrol
DB_PASSWORD=CHANGE_ME_SECURE_PASSWORD
DB_NAME=groundcontrol
DB_SSLMODE=disable
DB_MAX_OPEN_CONNS=20

# Authentication
ADMIN_USERNAME=admin
ADMIN_PASSWORD=CHANGE_ME_ADMIN_PASSWORD
JWT_SECRET=GENERATE_RANDOM_32_CHAR_STRING_HERE
JWT_EXPIRY=24h

# Harbor Integration
HARBOR_URL=https://harbor.example.com
HARBOR_USERNAME=robot$groundcontrol
HARBOR_PASSWORD=ROBOT_ACCOUNT_PASSWORD
HARBOR_SKIP_TLS_VERIFY=false
HARBOR_SYNC_INTERVAL=5m

# SPIFFE (optional, for zero-trust)
SPIFFE_ENABLED=false
SPIFFE_AGENT_SOCKET=/run/spire/sockets/agent.sock
EOF
```

**Important:** Replace all `CHANGE_ME_*` values with secure credentials.

### Step 2: Create Docker Compose File

```bash
cat > docker-compose.yml << 'EOF'
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    container_name: ground-control-db
    restart: unless-stopped
    environment:
      POSTGRES_DB: ${DB_NAME}
      POSTGRES_USER: ${DB_USER}
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${DB_USER} -d ${DB_NAME}"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - satellite-network

  ground-control:
    image: ghcr.io/container-registry/ground-control:latest
    container_name: ground-control-app
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    env_file:
      - configs/groundcontrol.env
    ports:
      - "8080:8080"
      - "8090:8090"  # Metrics port
    volumes:
      - ground-control-data:/data
      - /etc/time/timezone:/etc/timezone:ro
    networks:
      - satellite-network
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/api/v1/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  postgres-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: ./data/postgres
  
  ground-control-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: ./data/gc

networks:
  satellite-network:
    driver: bridge
EOF
```

### Step 3: Start Ground Control

```bash
# Create data directories
mkdir -p data/{postgres,gc}
chmod 755 data/{postgres,gc}

# Load environment
export $(cat configs/groundcontrol.env | xargs)

# Start services
docker-compose up -d postgres

# Wait for database
sleep 5

# Start Ground Control
docker-compose up -d ground-control

# Verify it's running
docker-compose ps
docker logs -f ground-control
```

**Expected logs:**
```
Running database migrations...
Migration 001 applied
Migration 002 applied
Server is listening on 0.0.0.0:8080
```

### Step 4: Verify Ground Control

```bash
# Health check
curl http://localhost:8080/api/v1/health

# Should return:
# {"status":"healthy","database":"connected","version":"0.1.0"}
```

---

## Part 2: Satellite Deployment (Edge Locations)

Deploy one Satellite instance at each edge location.

### Step 1: Create Satellite Configuration

The configuration below is an example only. Refer to the Configuration Reference for all supported options and recommended values.

```bash
cat > configs/satellite-config.yaml << 'EOF'
satellite:
  # Ground Control connection
  ground_control:
    url: http://ground-control:8080  # Or actual GC IP/hostname
    device_id: edge-site-01          # Change per satellite
    group: production-sites
    auth_token: sat_reg_your_token_here
    sync_interval: 5m
    timeout: 30s
    retry_attempts: 3

  # Logging
  log:
    level: info
    format: json
    output: stdout

  # Local registry
  registry:
    storage_path: /data/registry
    host: 0.0.0.0
    port: 5000
    mode: mirror
    allow_upstream_pull: true
    upstream_url: https://harbor.example.com
    upstream_timeout: 30s

  # Container runtime integration
  runtime:
    type: containerd           # or crio
    socket: /run/containerd/containerd.sock
    mirrors:
      enabled: true
      namespace: docker.io

  # Garbage collection
  gc:
    enabled: true
    strategy: lru
    target_percent: 80

  # Scheduling
  scheduler:
    max_concurrent_pulls: 4
    pull_timeout: 10m
EOF
```

### Step 2: Register Satellite with Ground Control

From your Ground Control host:

```bash
# Get auth token
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username":"admin",
    "password":"your_admin_password"
  }' | jq -r '.token')

# Register device
RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/devices/register \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"edge-site-01",
    "group":"production-sites",
    "description":"First edge location"
  }')

DEVICE_ID=$(echo $RESPONSE | jq -r '.id')
REG_TOKEN=$(echo $RESPONSE | jq -r '.registration_token')

echo "Device ID: $DEVICE_ID"
echo "Auth Token: $REG_TOKEN"

# Save for satellite config
sed -i "s|device_id: .*|device_id: $DEVICE_ID|" configs/satellite-config.yaml
sed -i "s|auth_token: .*|auth_token: $REG_TOKEN|" configs/satellite-config.yaml
```

### Step 3: Create Satellite Docker Compose

```bash
cat > docker-compose.satellite.yml << 'EOF'
version: '3.8'

services:
  satellite:
    image: ghcr.io/container-registry/satellite:latest
    container_name: harbor-satellite
    restart: unless-stopped
    
    environment:
      CONFIG_PATH: /etc/satellite/config.yaml
      LOG_LEVEL: info
    
    volumes:
      # Configuration
      - ./configs/satellite-config.yaml:/etc/satellite/config.yaml:ro
      
      # Data persistence
      - satellite-data:/data
      
      # Container runtime access
      - /run/containerd/containerd.sock:/run/containerd/containerd.sock
      - /etc/containerd/:/etc/containerd/
      
      # SPIFFE (optional)
      # - /run/spire/sockets:/run/spire/sockets:ro
    
    ports:
      - "5000:5000"          # Registry API
      - "8081:8081"          # Satellite API
      - "9090:9090"          # Metrics (Prometheus)
    
    networks:
      - satellite-edge-network
    
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:5000/v2/"]
      interval: 30s
      timeout: 10s
      retries: 3
    
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    
    # Resource limits (adjust for your hardware)
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 1G

volumes:
  satellite-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: ./data/satellite

networks:
  satellite-edge-network:
    driver: bridge
EOF
```

### Step 4: Start Satellite

```bash
# Create data directory
mkdir -p data/satellite
chmod 755 data/satellite

# Start Satellite
docker-compose -f docker-compose.satellite.yml up -d satellite

# Verify
docker-compose -f docker-compose.satellite.yml ps
docker-compose -f docker-compose.satellite.yml logs -f satellite
```

**Expected logs:**
```
Starting satellite...
Connected to ground-control at http://ground-control:8080
Successfully registered device: edge-site-01
Synchronization started...
```

---

## Production Considerations

For production environments, consider adding TLS, backups, and monitoring based on your operational requirements. Refer to your organization's security and operational practices for:

- **Networking**: Multi-site connectivity, DNS resolution, and firewall configuration
- **Security**: TLS termination, certificate management, and authentication
- **Backup**: Database backup and disaster recovery strategies  
- **Monitoring**: Application metrics, container health, and log management
- **Scale**: Load balancing, horizontal scaling, and resource planning

## Common Docker Deployment Issues

### Container startup failures
- Check container logs: `docker logs <container-name>`
- Verify configuration files and environment variables
- Ensure required ports are available

### Network connectivity issues  
- Verify Docker network configuration
- Test connectivity between containers
- Check firewall rules and port accessibility

### Storage and resource issues
- Monitor disk usage and clean up unused images
- Adjust resource limits based on workload requirements
- Ensure sufficient storage for registry data

## Next Steps

- See [Kubernetes Deployment](kubernetes.md) for container orchestration
- Review [Configuration Reference](../configuration.md) for advanced options
- Check [Troubleshooting Guide](../troubleshooting.md) for issue resolution
