# Getting Started with Harbor Satellite

This guide will help you get started with Harbor Satellite, from initial setup to running your first satellite instance.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Architecture Overview](#architecture-overview)
- [Quick Start](#quick-start)
- [Step-by-Step Setup](#step-by-step-setup)
  - [Setting Up Ground Control](#setting-up-ground-control)
  - [Setting Up Satellite](#setting-up-satellite)
- [Verifying Your Setup](#verifying-your-setup)
- [Next Steps](#next-steps)

## Overview

Harbor Satellite is a registry fleet management and artifact distribution solution that extends Harbor container registry to edge computing environments. It consists of two main components:

1. **Ground Control**: The central management service that orchestrates satellite configurations, manages artifact distribution, and handles satellite registration.
2. **Satellite**: A lightweight, standalone registry that runs at edge locations, acting as both a primary registry for local workloads and a fallback for the central Harbor instance.

## Prerequisites

Before you begin, ensure you have:

- **Harbor Registry**: A Harbor registry instance with the satellite adapter installed. You can find it in the [harbor-next satellite branch](https://github.com/container-registry/harbor-next/tree/satellite).
- **Credentials**: Harbor credentials with permission to create robot accounts in the registry.
- **Docker and Docker Compose**: For containerized deployments (recommended for end users).
- **Dagger** (Optional): For development builds. [Download and install Dagger](https://docs.dagger.io/install).
- **Go 1.24+** (Optional): For building from source.

## Architecture Overview

```
┌─────────────────┐
│  Harbor Registry│
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Ground Control  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│    Satellite    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Local Registry │
│     (Zot)       │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Local Workloads │
└─────────────────┘
```

For more detailed architecture information, see the [Architecture Documentation](architecture/overview.md).

## Quick Start

If you want to get up and running quickly, follow the [QUICKSTART.md](../../QUICKSTART.md) guide in the repository root. This guide provides a streamlined setup process.

## Step-by-Step Setup

### Setting Up Ground Control

Ground Control is the central service that manages satellite configurations. Follow these steps to set it up:

#### 1. Clone the Repository

```bash
git clone https://github.com/container-registry/harbor-satellite.git
cd harbor-satellite
cd ground-control
```

#### 2. Configure Ground Control

Navigate to the `ground-control` directory and create a `.env` file:

```bash
cd ground-control
cp .env.example .env
```

Edit the `.env` file with your configuration:

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

# Password Policy (optional)
PASSWORD_MIN_LENGTH=8
PASSWORD_MAX_LENGTH=128
PASSWORD_REQUIRE_UPPERCASE=true
PASSWORD_REQUIRE_LOWERCASE=true
PASSWORD_REQUIRE_NUMBER=true
PASSWORD_REQUIRE_SPECIAL=false

# Database Configuration
DB_HOST=postgres  # Use 'pgservice' for Dagger, '127.0.0.1' for local
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres
DB_PASSWORD=password
```

> **Note**: By default, passwords must be at least 8 characters and contain uppercase, lowercase, and a number. You can customize this via the password policy settings.

#### 3. Start Ground Control

**Option A: Using Docker Compose (Recommended)**

```bash
docker compose up -d
```

**Option B: Using Dagger (For Developers)**

```bash
dagger call run-ground-control up
```

**Option C: Build and Run Binary**

```bash
# Build
dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev

# Run
./gc-dev
```

#### 4. Verify Ground Control

Check if Ground Control is running:

```bash
curl http://localhost:9090/health
```

A `200 OK` response indicates Ground Control is healthy.

#### 5. Login to Ground Control

Authenticate with the admin credentials to get a session token:

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

> **Note**: The Bearer token is valid for 24 hours by default. Configure `SESSION_DURATION_HOURS` in `.env` to change this.

### Setting Up Satellite

#### 1. Create a Group for Artifacts

A **group** is a set of images that the satellite needs to replicate from the upstream registry. Create a group:

```bash
curl -X POST http://localhost:9090/groups/sync \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "group": "group1",
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

> **Note**: Replace `repository`, `tag`, and `digest` with your artifact details. Use `docker inspect` or Harbor's UI to find the digest.

#### 2. Configure the Satellite

Create a config artifact for the satellite. This tells the satellite where Ground Control is located and defines how to replicate artifacts:

```bash
curl -X POST http://localhost:9090/configs \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "config_name": "config1",
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

> **Tip**: Adjust `ground_control_url` and `local_registry.url` if running on a different host or port.

#### 3. Register the Satellite

Register the satellite with the group and configuration created earlier:

```bash
curl -X POST http://localhost:9090/satellites \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "satellite_1",
    "groups": ["group1"],
    "config_name": "config1"
  }'
```

> **Important**: Copy the satellite token from the response. This is different from your admin session token.

#### 4. Start the Satellite

Set up the satellite using the satellite token from Step 3.

**Option A: Using Docker Compose (Recommended)**
(You should be in the root of the  directory)

```bash
TOKEN=<satellite-token> docker compose up -d
```

**Option B: Using Dagger**

```bash
# Build
dagger call build --source=. --component=satellite export --path=./bin

# Run
./bin --token "<satellite-token>" --ground-control-url "http://host.docker.internal:9090"
```

**Option C: Using Go**

```bash
go run cmd/main.go --token "<satellite-token>" --ground-control-url "<ground control url here>"
```

> **Note**: By default, logging in JSON format is set to true. To change this, pass the additional flag `--json-logging=false`.

#### 5. Configure Local Registry as Mirror (Optional)

Harbor Satellite allows you to set up a local registry as a mirror for upstream registries. Using the optional `--mirrors` flag, you can specify which upstream registries should be mirrored:

```bash
--mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io
```

**Supported CRIs:**
- `docker`
- `crio`
- `podman`
- `containerd`

**Notes:**
- When using docker as a runtime, it supports mirroring images from docker.io. Use `--mirrors=docker:true` to enable Docker mirroring.
- For loading dockerd's configs, the docker service is restarted. Make sure you have stopped all other docker processes.
- Appending or updating CRI configuration files requires sudo.
- Satellite assumes default configuration paths for each CRI. If you use non-standard locations, you may need to manually update the configs.
- Containerd: Using outdated versions is not recommended, as some configuration options and styles may be deprecated.

## Verifying Your Setup

### Check Ground Control Health

```bash
curl http://localhost:9090/health
```

### Check Satellite Status

```bash
curl -X GET http://localhost:9090/satellites/satellite_1/status \
  -H "Authorization: Bearer $TOKEN"
```

### Check Local Registry

```bash
curl http://localhost:8585/v2/_catalog
```

### Verify Image Replication

Check if images are being replicated to the local registry:

```bash
curl http://localhost:8585/v2/library/alpine/tags/list
```

## Next Steps

Now that you have Harbor Satellite up and running, you can:

1. **Learn about Configuration**: See the [Configuration Reference](configuration.md) for detailed configuration options.
2. **Explore the API**: Check the [API Reference](api-reference.md) for all available Ground Control endpoints.
3. **Understand the Architecture**: Read the [Architecture Documentation](architecture/overview.md) for a deeper understanding.
4. **Deploy in Production**: Follow the [Deployment Guides](deployment/) for Docker, Kubernetes, or systemd deployments.
5. **Troubleshoot Issues**: Refer to the [Troubleshooting Guide](troubleshooting.md) if you encounter any problems.

## Additional Resources

- [QUICKSTART.md](../../QUICKSTART.md) - Quick start guide
- [README.md](../../README.md) - Project overview
- [Architecture Documentation](architecture/) - Detailed architecture information
- [Configuration Reference](configuration.md) - Complete configuration options
- [API Reference](api-reference.md) - Ground Control API documentation

## Getting Help

- Explore the [Harbor Satellite documentation](https://docs.goharbor.io)
- Join the [Harbor community](https://community.goharbor.io) for support
- Open an issue on GitHub: https://github.com/container-registry/harbor-satellite/issues
- Join the [#harbor-satellite channel on CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1)
