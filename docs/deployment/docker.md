# Docker Deployment

This guide covers deploying Harbor Satellite using Docker and Docker Compose for both development and production environments.

## Prerequisites

- Docker Engine 20.10+
- Docker Compose 2.0+
- At least 4GB RAM available
- 20GB free disk space for container images

## Quick Start with Docker Compose

### Ground Control Deployment

1. **Clone and navigate**:
   ```bash
   git clone https://github.com/container-registry/harbor-satellite.git
   cd harbor-satellite/ground-control
   ```

2. **Configure environment**:
   ```bash
   cp .env.example .env
   # Edit .env with your Harbor credentials and settings
   ```

3. **Start services**:
   ```bash
   docker compose up -d
   ```

4. **Verify deployment**:
   ```bash
   docker compose ps
   curl http://localhost:9090/health
   ```

### Satellite Deployment

1. **Navigate to root directory**:
   ```bash
   cd ..
   ```

2. **Start satellite** (after configuring Ground Control):
   ```bash
   TOKEN=<satellite-token> docker compose up -d
   ```

3. **Verify satellite**:
   ```bash
   docker compose ps
   curl http://localhost:8585/v2/
   ```

## Production Deployment

### Ground Control Production Setup

Create a production `docker-compose.yml`:

```yaml
version: '3.8'

services:
  ground-control:
    image: harbor-satellite/ground-control:latest
    ports:
      - "9090:9090"
    environment:
      - HARBOR_URL=https://harbor.example.com
      - HARBOR_USERNAME=robot$account
      - HARBOR_PASSWORD=secure-token
      - ADMIN_PASSWORD=SecureProdPass123
      - DB_HOST=postgres
      - DB_PORT=5432
      - DB_DATABASE=groundcontrol
      - DB_USERNAME=groundcontrol
      - DB_PASSWORD=secure-db-password
      - SESSION_DURATION_HOURS=24
      - LOCKOUT_DURATION_MINUTES=15
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9090/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    restart: unless-stopped

  postgres:
    image: postgres:15-alpine
    environment:
      - POSTGRES_DB=groundcontrol
      - POSTGRES_USER=groundcontrol
      - POSTGRES_PASSWORD=secure-db-password
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./sql:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U groundcontrol"]
      interval: 30s
      timeout: 10s
      retries: 3
    restart: unless-stopped

volumes:
  postgres_data:
```

### Satellite Production Setup

Create a production satellite `docker-compose.yml`:

```yaml
version: '3.8'

services:
  satellite:
    image: harbor-satellite/satellite:latest
    environment:
      - SATELLITE_TOKEN=<your-satellite-token>
      - GROUND_CONTROL_URL=https://ground-control.example.com
      - LOG_LEVEL=info
      - JSON_LOGGING=true
    volumes:
      - ./zot:/var/lib/zot
      - ./config:/etc/satellite
    ports:
      - "8585:8585"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8585/v2/"]
      interval: 60s
      timeout: 10s
      retries: 3
    restart: unless-stopped
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp
```

## Docker Compose Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `HARBOR_URL` | Harbor registry URL | - | Yes |
| `HARBOR_USERNAME` | Robot account username | - | Yes |
| `HARBOR_PASSWORD` | Robot account password | - | Yes |
| `ADMIN_PASSWORD` | Ground Control admin password | - | Yes |
| `DB_HOST` | PostgreSQL host | postgres | No |
| `DB_PORT` | PostgreSQL port | 5432 | No |
| `DB_DATABASE` | Database name | groundcontrol | No |
| `DB_USERNAME` | Database username | postgres | No |
| `DB_PASSWORD` | Database password | password | No |
| `SATELLITE_TOKEN` | Satellite authentication token | - | Yes |
| `GROUND_CONTROL_URL` | Ground Control URL | - | Yes |
| `LOG_LEVEL` | Logging level | info | No |
| `JSON_LOGGING` | Enable JSON logging | true | No |

### Volumes

| Volume | Purpose | Recommended Size |
|--------|---------|------------------|
| `postgres_data` | Database persistence | 10GB+ |
| `./zot` | Registry storage | 50GB+ |
| `./config` | Satellite configuration | 1GB |

### Networking

- **Ground Control**: Port 9090 (HTTP)
- **Satellite Registry**: Port 8585 (HTTP)
- **PostgreSQL**: Internal only (5432)

## Security Considerations

### Container Security

```yaml
services:
  satellite:
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp
    user: 1000:1000
```

### Network Security

```yaml
services:
  ground-control:
    networks:
      - ground-control-net
    # No external ports exposed

networks:
  ground-control-net:
    internal: true
```

### Secrets Management

Use Docker secrets for sensitive data:

```yaml
secrets:
  harbor_password:
    file: ./secrets/harbor_password.txt

services:
  ground-control:
    secrets:
      - harbor_password
```

## Monitoring and Logging

### Health Checks

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:9090/health"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 40s
```

### Logging Configuration

```yaml
services:
  ground-control:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

### Metrics Collection

Enable Prometheus metrics:

```yaml
services:
  satellite:
    environment:
      - METRICS_ENABLED=true
      - METRICS_PORT=9091
    ports:
      - "9091:9091"
```

## Scaling and High Availability

### Load Balancing

```yaml
services:
  ground-control:
    deploy:
      replicas: 3
      resources:
        limits:
          cpus: '1.0'
          memory: 1G
        reservations:
          cpus: '0.5'
          memory: 512M
```

### Database High Availability

Use PostgreSQL with replication:

```yaml
services:
  postgres-primary:
    image: postgres:15-alpine
    # Primary configuration

  postgres-replica:
    image: postgres:15-alpine
    # Replica configuration
```

## Backup and Recovery

### Database Backup

```bash
# Create backup
docker exec postgres pg_dump -U groundcontrol groundcontrol > backup.sql

# Restore backup
docker exec -i postgres psql -U groundcontrol groundcontrol < backup.sql
```

### Registry Backup

```bash
# Backup Zot storage
docker run --rm -v satellite_zot:/data -v $(pwd):/backup alpine tar czf /backup/zot-backup.tar.gz -C /data .
```

### Automated Backups

```yaml
services:
  backup:
    image: postgres:15-alpine
    command: >
      sh -c "while true; do
        pg_dump -U groundcontrol groundcontrol > /backup/backup_$(date +%Y%m%d_%H%M%S).sql
        sleep 86400
      done"
    volumes:
      - postgres_data:/var/lib/postgresql/data:ro
      - ./backups:/backup
```

## Troubleshooting Docker Deployments

### Common Issues

1. **Port conflicts**:
   ```bash
   docker ps -a | grep -E ":(9090|8585)"
   ```

2. **Permission issues**:
   ```bash
   ls -la ./zot
   # Ensure correct ownership
   ```

3. **Resource constraints**:
   ```bash
   docker stats
   ```

4. **Network issues**:
   ```bash
   docker network ls
   docker network inspect bridge
   ```

### Debug Commands

```bash
# Check container logs
docker compose logs -f ground-control

# Execute into container
docker compose exec ground-control sh

# Check resource usage
docker stats

# Network debugging
docker compose exec ground-control nslookup harbor.example.com
```

## Performance Tuning

### Resource Limits

```yaml
services:
  satellite:
    deploy:
      resources:
        limits:
          cpus: '2.0'
          memory: 4G
        reservations:
          cpus: '1.0'
          memory: 2G
```

### Storage Optimization

```yaml
services:
  satellite:
    environment:
      - ZOT_STORAGE_DEDUPE=true
      - ZOT_STORAGE_GC=true
      - ZOT_STORAGE_GC_DELAY=24h
```

### Network Optimization

```yaml
services:
  ground-control:
    networks:
      ground-control-net:
        driver_opts:
          com.docker.network.bridge.name: ground-control-br
```

## Updates and Maintenance

### Rolling Updates

```bash
# Update images
docker compose pull

# Rolling restart
docker compose up -d --no-deps ground-control
```

### Zero-Downtime Updates

```yaml
services:
  ground-control:
    deploy:
      update_config:
        parallelism: 1
        delay: 10s
      restart_policy:
        condition: on-failure
```</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/deployment/docker.md