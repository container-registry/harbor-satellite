# Getting Started

This guide will walk you through setting up Harbor Satellite for the first time, including both Ground Control (central management) and Satellite (edge registry) components.

## Prerequisites

Before you begin, ensure you have the following:

- **Harbor Registry**: A Harbor instance with the satellite adapter installed (available in the [harbor-next satellite branch](https://github.com/container-registry/harbor-next/tree/satellite))
- **Credentials**: Harbor admin credentials with permission to create robot accounts
- **Dagger**: Latest version installed ([download here](https://docs.dagger.io/install))
- **Docker & Docker Compose**: For non-Dagger setups ([install Docker](https://docs.docker.com/get-docker/))
- **Go 1.21+**: For development builds

## Architecture Overview

Harbor Satellite consists of two main components:

1. **Ground Control**: Central management service that orchestrates satellite configurations and artifact distribution
2. **Satellite**: Edge registry that runs locally and synchronizes artifacts from the central Harbor registry

## Quick Start

### Step 1: Set Up Ground Control

Ground Control is the central management component that controls satellite configurations and artifact distribution.

1. **Clone the repository**:
   ```bash
   git clone https://github.com/container-registry/harbor-satellite.git
   cd harbor-satellite
   ```

2. **Navigate to Ground Control**:
   ```bash
   cd ground-control
   ```

3. **Configure environment**:
   ```bash
   cp .env.example .env
   ```

   Edit `.env` with your settings:
   ```env
   # Harbor Registry Credentials
   HARBOR_USERNAME=admin
   HARBOR_PASSWORD=Harbor12345
   HARBOR_URL=http://localhost:8080

   # Ground Control Settings
   PORT=9090
   ADMIN_PASSWORD=SecurePass123
   SESSION_DURATION_HOURS=24

   # Database (for Docker Compose)
   DB_HOST=127.0.0.1
   DB_PORT=5432
   DB_DATABASE=groundcontrol
   DB_USERNAME=postgres
   DB_PASSWORD=password
   ```

4. **Start Ground Control**:
   ```bash
   docker compose up -d
   ```

5. **Verify health**:
   ```bash
   curl http://localhost:9090/health
   ```

### Step 2: Authenticate with Ground Control

Get an authentication token for API access:

```bash
curl -X POST http://localhost:9090/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "SecurePass123"
  }'
```

Save the token from the response:
```bash
export TOKEN="<token-from-response>"
```

### Step 3: Create an Artifact Group

Create a group containing the artifacts to be replicated:

```bash
curl -X POST http://localhost:9090/groups/sync \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "group": "my-group",
    "registry": "http://localhost:8080",
    "artifacts": [
      {
        "repository": "library/alpine",
        "tag": ["latest"],
        "type": "docker",
        "digest": "sha256:5a6ee6c36824d527a0fe91a2a7c160c2e286bbeae46cd931c337ac769f1bd930",
        "deleted": false
      }
    ]
  }'
```

### Step 4: Create Satellite Configuration

Create a configuration for the satellite:

```bash
curl -X POST http://localhost:9090/configs \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "config_name": "satellite-config",
    "config": {
      "state_config": {},
      "app_config": {
        "ground_control_url": "http://host.docker.internal:9090",
        "log_level": "info",
        "use_unsecure": true,
        "state_replication_interval": "@every 00h00m10s",
        "register_satellite_interval": "@every 00h00m10s",
        "local_registry": {
          "url": "http://0.0.0.0:8585"
        },
        "heartbeat_interval": "@every 00h00m30s"
      },
      "zot_config": {
        "distSpecVersion": "1.1.0",
        "storage": {
          "rootDirectory": "./zot"
        },
        "http": {
          "address": "0.0.0.0",
          "port": "8585"
        },
        "log": {
          "level": "info"
        }
      }
    }
  }'
```

### Step 5: Register Satellite

Register the satellite with Ground Control:

```bash
curl -X POST http://localhost:9090/satellites \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "my-satellite",
    "groups": ["my-group"],
    "config_name": "satellite-config"
  }'
```

Save the satellite token from the response.

### Step 6: Start Satellite

Start the satellite using the token:

```bash
TOKEN=<satellite-token> docker compose up -d
```

### Step 7: Configure Container Runtime (Optional)

Configure your container runtime to use the local satellite registry as a mirror:

```bash
# For containerd
docker run --rm -v /etc/containerd:/etc/containerd \
  harbor-satellite --mirrors=containerd:docker.io,quay.io

# For Docker
docker run --rm -v /etc/docker:/etc/docker \
  harbor-satellite --mirrors=docker:true
```

## Verification

1. **Check Ground Control health**:
   ```bash
   curl http://localhost:9090/health
   ```

2. **Check Satellite status**:
   ```bash
   curl http://localhost:9090/satellites/my-satellite/status \
     -H "Authorization: Bearer $TOKEN"
   ```

3. **Verify local registry**:
   ```bash
   curl http://localhost:8585/v2/
   ```

4. **Test image pull**:
   ```bash
   docker pull localhost:8585/library/alpine:latest
   ```

## Next Steps

- [Configure additional satellites](configuration.md)
- [Set up monitoring](deployment/docker.md)
- [Troubleshoot common issues](troubleshooting.md)
- [Explore API endpoints](api-reference.md)

## Development Setup

For development, use Dagger for building and testing:

```bash
# Build satellite
dagger call build --source=. --component=satellite export --path=./bin

# Run tests
dagger call test

# Start development environment
dagger call run-ground-control up
```</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/getting-started.md