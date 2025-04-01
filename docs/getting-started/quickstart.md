# Quick Start Guide

This guide will help you get started with Harbor Satellite quickly. Harbor Satellite is a solution that brings the power of Harbor container registry to edge computing environments, enabling efficient artifact distribution and management.

## What is Harbor Satellite?

Harbor Satellite is a lightweight, standalone registry solution designed for edge computing environments. It acts as both a primary registry for local workloads and a fallback for the central Harbor instance. Key features include:

- Fleet management for edge registries
- Centralized artifact distribution management
- Predictable behavior in challenging connectivity situations
- Optimized resource and bandwidth utilization
- Air-gapped deployment capability

## Prerequisites

Before you begin, ensure you have:

1. A Harbor registry instance (or similar OCI-compliant registry)
2. Credentials with permission to create robot accounts in the registry
3. The latest version of Dagger installed
4. Docker installed (for local development)

## Quick Setup

### 1. Set Up Ground Control

First, set up the Ground Control component which manages your satellite fleet:

```bash
# Set required environment variables
export HARBOR_USERNAME=admin
export HARBOR_PASSWORD=your_password
export HARBOR_URL=your_harbor_url
export PORT=8080
export APP_ENV=local

# Start Ground Control using Dagger
dagger call run-ground-control up
```

### 2. Register a Satellite

Create a group and register a satellite with Ground Control:

```bash
# Create a group
curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
--data '{
  "group": "my-group",
  "registry": "your_registry_url",
  "artifacts": [
    {
      "repository": "your_project/your_image",
      "tag": ["latest"],
      "type": "docker",
      "digest": "your_image_digest",
      "deleted": false
    }
  ]
}'

# Register a satellite
curl --location 'http://localhost:8080/satellites/register' \
--header 'Content-Type: application/json' \
--data '{
    "name": "my-satellite",
    "groups": ["my-group"]
}'
```

### 3. Configure and Start Satellite

Create a `config.json` file with the following content:

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
      },
      {
        "name": "register_satellite",
        "schedule": "@every 00h00m05s"
      }
    ]
  }
}
```

Start the satellite:

```bash
# Build the satellite binary
dagger call build --source=. --component=satellite export --path=./bin

# Run the satellite
./bin/satellite
```

## Next Steps

1. [Installation Guide](installation.md) - Detailed installation instructions
2. [Basic Concepts](concepts.md) - Learn about core concepts
3. [Configuration Guide](../user-guide/configuration.md) - Configure your deployment
4. [User Guide](../user-guide/README.md) - Learn how to use Harbor Satellite

## Common Use Cases

1. **Edge Computing**: Deploy containerized applications in remote locations
2. **Air-Gapped Environments**: Run applications in isolated networks
3. **Global Distribution**: Efficiently distribute images across multiple regions
4. **High Availability**: Ensure image availability close to workloads

## Getting Help

- Join the [CNCF Slack channel](https://cloud-native.slack.com/archives/C06NE6EJBU1)
- Check the [GitHub Issues](https://github.com/goharbor/harbor-satellite/issues)