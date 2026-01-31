# Docker Deployment Guide

This guide covers deploying Harbor Satellite using Docker and Docker Compose.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Ground Control Deployment](#ground-control-deployment)
- [Satellite Deployment](#satellite-deployment)
- [Complete Setup Example](#complete-setup-example)
- [Production Considerations](#production-considerations)
- [Troubleshooting](#troubleshooting)

## Prerequisites

- Docker 20.10 or later
- Docker Compose 2.0 or later
- Harbor registry with satellite extension
- Network access to Harbor registry

## Ground Control Deployment

### Using Docker Compose (Recommended)

1. **Navigate to Ground Control Directory:**
   ```bash
   cd ground-control
   ```

2. **Create Environment File:**
   ```bash
   cp .env.example .env
   ```

3. **Edit `.env` File:**
   ```env
   # Harbor Registry Credentials
   HARBOR_USERNAME=admin
   HARBOR_PASSWORD=Harbor12345
   HARBOR_URL=http://localhost:8080

   # Ground Control Settings
   PORT=9090
   ADMIN_PASSWORD=SecurePass123
   SESSION_DURATION_HOURS=24
   LOCKOUT_DURATION_MINUTES=15

   # Database Configuration
   DB_HOST=postgres
   DB_PORT=5432
   DB_DATABASE=groundcontrol
   DB_USERNAME=postgres
   DB_PASSWORD=password
   ```

4. **Start Services:**
   ```bash
   docker compose up -d
   ```

5. **Verify Deployment:**
   ```bash
   # Check services are running
   docker compose ps

   # Check health
   curl http://localhost:9090/health

   # Check logs
   docker compose logs -f groundcontrol
   ```

### Using Docker Run

1. **Start PostgreSQL:**
   ```bash
   docker run -d \
     --name groundcontrol-db \
     -e POSTGRES_DB=groundcontrol \
     -e POSTGRES_USER=postgres \
     -e POSTGRES_PASSWORD=password \
     -p 8100:5432 \
     postgres:16-alpine
   ```

2. **Build Ground Control Image:**
   ```bash
   docker build -t harbor-satellite-gc:latest -f ground-control/Dockerfile .
   ```

3. **Run Ground Control:**
   ```bash
   docker run -d \
     --name groundcontrol \
     --link groundcontrol-db:postgres \
     -e HARBOR_USERNAME=admin \
     -e HARBOR_PASSWORD=Harbor12345 \
     -e HARBOR_URL=http://localhost:8080 \
     -e PORT=9090 \
     -e ADMIN_PASSWORD=SecurePass123 \
     -e DB_HOST=postgres \
     -e DB_PORT=5432 \
     -e DB_DATABASE=groundcontrol \
     -e DB_USERNAME=postgres \
     -e DB_PASSWORD=password \
     -p 9090:9090 \
     harbor-satellite-gc:latest
   ```

### Docker Compose Configuration

The `ground-control/docker-compose.yml` includes:

- **PostgreSQL Service**: Database for Ground Control
- **Ground Control Service**: Main API server
- **Volume Management**: Persistent database storage
- **Health Checks**: Automatic service health monitoring

## Satellite Deployment

### Using Docker Compose (Recommended)

1. **Create Environment File:**
   ```bash
   # In repository root
   cp .env.example .env
   ```

2. **Edit `.env` File:**
   ```env
   GROUND_CONTROL_URL=http://host.docker.internal:9090
   TOKEN=<satellite-token-from-registration>
   USE_UNSECURE=false
   ```

3. **Start Satellite:**
   ```bash
   docker compose up -d
   ```

4. **Verify Deployment:**
   ```bash
   # Check service is running
   docker compose ps

   # Check logs
   docker compose logs -f satellite

   # Check local registry
   curl http://localhost:8585/v2/_catalog
   ```

### Using Docker Run

1. **Build Satellite Image:**
   ```bash
   docker build -t harbor-satellite:latest .
   ```

2. **Run Satellite:**
   ```bash
   docker run -d \
     --name satellite \
     -e GROUND_CONTROL_URL=http://host.docker.internal:9090 \
     -e TOKEN=<satellite-token> \
     -e USE_UNSECURE=false \
     -p 8090:8080 \
     -p 8585:8585 \
     -v satellite-data:/app/zot \
     --add-host=host.docker.internal:host-gateway \
     harbor-satellite:latest
   ```

### Docker Compose Configuration

The `docker-compose.yml` includes:

- **Satellite Service**: Main satellite process
- **Port Mapping**: 
  - `8090:8080` - Satellite API/metrics
  - `8585:8585` - Local registry (Zot)
- **Host Gateway**: Access to host services (Ground Control)
- **Restart Policy**: Automatic restart on failure

## Complete Setup Example

### Step 1: Start Ground Control

```bash
cd ground-control
cp .env.example .env
# Edit .env with your configuration
docker compose up -d
```

### Step 2: Register Satellite

```bash
# Login to Ground Control
TOKEN=$(curl -X POST http://localhost:9090/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"SecurePass123"}' \
  | jq -r .token)

# Create group
curl -X POST http://localhost:9090/groups/sync \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "group": "group1",
    "registry": "http://localhost:8080",
    "artifacts": [...]
  }'

# Create config
curl -X POST http://localhost:9090/configs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config_name": "config1",
    "config": {...}
  }'

# Register satellite
SAT_TOKEN=$(curl -X POST http://localhost:9090/satellites \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "satellite_1",
    "groups": ["group1"],
    "config_name": "config1"
  }' | jq -r .token)
```

### Step 3: Start Satellite

```bash
cd ..
echo "TOKEN=$SAT_TOKEN" >> .env
docker compose up -d
```

## Production Considerations

### Security

1. **Use HTTPS:**
   ```yaml
   # In docker-compose.yml
   services:
     groundcontrol:
       environment:
         - APP_ENV=production
       # Add TLS configuration
   ```

2. **Secure Database:**
   ```yaml
   services:
     postgres:
       environment:
         - POSTGRES_PASSWORD_FILE=/run/secrets/db_password
       secrets:
         - db_password
   ```

3. **Network Isolation:**
   ```yaml
   services:
     groundcontrol:
       networks:
         - internal
     satellite:
       networks:
         - internal
   ```

### Resource Limits

```yaml
services:
  groundcontrol:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 1G
        reservations:
          cpus: '1'
          memory: 512M

  satellite:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 1G
```

### Persistent Storage

```yaml
services:
  postgres:
    volumes:
      - groundcontrol-db-data:/var/lib/postgresql/data

  satellite:
    volumes:
      - satellite-zot-data:/app/zot
      - satellite-config:/app/config.json

volumes:
  groundcontrol-db-data:
  satellite-zot-data:
  satellite-config:
```

### Health Checks

```yaml
services:
  groundcontrol:
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9090/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  satellite:
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
```

### Logging

```yaml
services:
  groundcontrol:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  satellite:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

### Monitoring

```yaml
services:
  groundcontrol:
    labels:
      - "prometheus.scrape=true"
      - "prometheus.port=9090"

  satellite:
    labels:
      - "prometheus.scrape=true"
      - "prometheus.port=8080"
```

## Troubleshooting

### Common Issues

1. **Port Conflicts:**
   ```bash
   # Check port usage
   lsof -i :9090
   lsof -i :8585
   
   # Change ports in docker-compose.yml if needed
   ```

2. **Network Issues:**
   ```bash
   # Verify host.docker.internal works
   docker run --rm curlimages/curl curl http://host.docker.internal:9090/health
   
   # For Linux, add to docker-compose.yml:
   extra_hosts:
     - "host.docker.internal:host-gateway"
   ```

3. **Volume Permissions:**
   ```bash
   # Fix volume permissions
   sudo chown -R $(id -u):$(id -g) ./zot
   ```

4. **Database Connection:**
   ```bash
   # Check database is accessible
   docker exec -it groundcontrol-db psql -U postgres -d groundcontrol -c "SELECT 1;"
   ```

### Debugging

1. **View Logs:**
   ```bash
   # All services
   docker compose logs -f
   
   # Specific service
   docker compose logs -f groundcontrol
   docker compose logs -f satellite
   ```

2. **Execute Commands:**
   ```bash
   # Shell into container
   docker compose exec groundcontrol sh
   docker compose exec satellite sh
   ```

3. **Inspect Containers:**
   ```bash
   # Container details
   docker compose ps
   docker inspect satellite
   ```

## Related Documentation

- [Getting Started Guide](../getting-started.md)
- [Configuration Reference](../configuration.md)
- [Troubleshooting Guide](../troubleshooting.md)
