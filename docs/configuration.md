# Configuration Reference

This document provides a comprehensive reference for all configuration options available in Harbor Satellite.

## Table of Contents

- [Overview](#overview)
- [Satellite Configuration](#satellite-configuration)
  - [State Config](#state-config)
  - [App Config](#app-config)
  - [Zot Config](#zot-config)
- [Ground Control Configuration](#ground-control-configuration)
- [Environment Variables](#environment-variables)
- [CLI Flags](#cli-flags)
- [Configuration Examples](#configuration-examples)

## Overview

Harbor Satellite uses JSON configuration files for the satellite component and environment variables for Ground Control. The satellite configuration is managed through Ground Control and can be hot-reloaded without restarting the satellite.

## Satellite Configuration

The satellite configuration is a JSON file with three main sections: `state_config`, `app_config`, and `zot_config`.

### State Config

The `state_config` section contains registry credentials and state URL. This section is typically managed by Ground Control and should not be edited manually.

```json
{
  "state_config": {
    "auth": {
      "url": "https://demo.goharbor.io",
      "username": "robot_account_name",
      "password": "robot_account_secret"
    },
    "state": "https://demo.goharbor.io/satellite/satellite-state/demo-sat/state:latest"
  }
}
```

**Fields:**
- `auth.url` (string, optional): URL of the Harbor registry
- `auth.username` (string, optional): Robot account username for authentication
- `auth.password` (string, optional): Robot account password/secret
- `state` (string, optional): URL to the state artifact in Harbor

> **Note**: The `state_config` section is read-only and managed by Ground Control. Do not edit this section manually.

### App Config

The `app_config` section contains application-level settings for the satellite.

```json
{
  "app_config": {
    "ground_control_url": "http://127.0.0.1:9090",
    "log_level": "info",
    "use_unsecure": false,
    "state_replication_interval": "@every 00h00m30s",
    "register_satellite_interval": "@every 00h00m05s",
    "heartbeat_interval": "@every 00h01m00s",
    "local_registry": {
      "url": "http://127.0.0.1:8585"
    },
    "bring_own_registry": false,
    "disable_heartbeat": false,
    "metrics": {
      "collect_cpu": true,
      "collect_memory": true,
      "collect_storage": true
    }
  }
}
```

**Fields:**

- `ground_control_url` (string, required): URL of the Ground Control service
  - Default: `http://127.0.0.1:8080`
  - Example: `http://host.docker.internal:9090`

- `log_level` (string, optional): Logging level
  - Valid values: `debug`, `info`, `warn`, `error`, `fatal`, `panic`
  - Default: `info`

- `use_unsecure` (boolean, optional): Use insecure (HTTP) connections to registries
  - Default: `false`
  - Set to `true` for HTTP-only registries (not recommended for production)

- `state_replication_interval` (string, optional): Cron expression for state replication interval
  - Default: `@every 00h00m30s`
  - Format: Standard cron expression or `@every` format
  - Examples:
    - `@every 00h00m10s` - Every 10 seconds
    - `@every 00h01m00s` - Every 1 minute
    - `0 */5 * * * *` - Every 5 minutes (standard cron)

- `register_satellite_interval` (string, optional): Cron expression for satellite registration interval
  - Default: `@every 00h00m05s`
  - Format: Standard cron expression or `@every` format

- `heartbeat_interval` (string, optional): Cron expression for heartbeat interval
  - Default: `@every 00h01m00s`
  - Format: Standard cron expression or `@every` format

- `local_registry.url` (string, optional): URL of the local registry
  - Default: `http://127.0.0.1:8585`
  - Used when `bring_own_registry` is `true`

- `bring_own_registry` (boolean, optional): Use an external registry instead of embedded Zot
  - Default: `false`
  - When `true`, you must provide `local_registry` credentials

- `disable_heartbeat` (boolean, optional): Disable heartbeat reporting to Ground Control
  - Default: `false`
  - Not recommended for production

- `metrics.collect_cpu` (boolean, optional): Collect CPU metrics
  - Default: `true`

- `metrics.collect_memory` (boolean, optional): Collect memory metrics
  - Default: `true`

- `metrics.collect_storage` (boolean, optional): Collect storage metrics
  - Default: `true`

### Zot Config

The `zot_config` section contains the configuration for the embedded Zot registry. This section is only used when `bring_own_registry` is `false`.

```json
{
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
```

**Fields:**

- `distSpecVersion` (string, required): OCI Distribution Specification version
  - Default: `"1.1.0"`

- `storage.rootDirectory` (string, required): Root directory for storing images
  - Default: `"./zot"`
  - Example: `"/var/lib/harbor-satellite/zot"`

- `http.address` (string, required): HTTP server address
  - Default: `"0.0.0.0"`
  - Use `"0.0.0.0"` to listen on all interfaces

- `http.port` (string, required): HTTP server port
  - Default: `"8585"`
  - Must be a valid port number

- `log.level` (string, optional): Logging level for Zot
  - Valid values: `debug`, `info`, `warn`, `error`
  - Default: `"info"`

**Additional Zot Configuration:**

Zot supports many additional configuration options. Refer to the [Zot documentation](https://zotregistry.io/docs/latest/) for complete configuration options. Common options include:

- `storage.gc`: Garbage collection settings
- `storage.dedupe`: Deduplication settings
- `http.tls`: TLS configuration
- `http.auth`: Authentication configuration
- `extensions`: Extension configurations

## Ground Control Configuration

Ground Control is configured using environment variables. Create a `.env` file in the `ground-control` directory.

### Harbor Registry Settings

```env
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=http://localhost:8080
```

- `HARBOR_USERNAME` (required): Harbor administrator username
- `HARBOR_PASSWORD` (required): Harbor administrator password
- `HARBOR_URL` (required): Harbor registry URL

### Server Settings

```env
PORT=9090
APP_ENV=production
```

- `PORT` (optional): Port for the Ground Control server
  - Default: `9090`

- `APP_ENV` (optional): Application environment
  - Valid values: `development`, `production`
  - Default: `production`

### Database Settings

```env
DB_HOST=postgres
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres
DB_PASSWORD=password
```

- `DB_HOST` (required): PostgreSQL host
  - Use `postgres` for Docker Compose
  - Use `pgservice` for Dagger
  - Use `127.0.0.1` for local PostgreSQL

- `DB_PORT` (optional): PostgreSQL port
  - Default: `5432`

- `DB_DATABASE` (required): Database name
  - Default: `groundcontrol`

- `DB_USERNAME` (required): Database username
  - Default: `postgres`

- `DB_PASSWORD` (required): Database password

### Authentication Settings

```env
ADMIN_PASSWORD=SecurePass123
SESSION_DURATION_HOURS=24
LOCKOUT_DURATION_MINUTES=15
```

- `ADMIN_PASSWORD` (required): Initial admin password
  - Must meet password policy requirements

- `SESSION_DURATION_HOURS` (optional): Session token validity duration
  - Default: `24`

- `LOCKOUT_DURATION_MINUTES` (optional): Account lockout duration after failed login attempts
  - Default: `15`

### Password Policy

```env
PASSWORD_MIN_LENGTH=8
PASSWORD_MAX_LENGTH=128
PASSWORD_REQUIRE_UPPERCASE=true
PASSWORD_REQUIRE_LOWERCASE=true
PASSWORD_REQUIRE_NUMBER=true
PASSWORD_REQUIRE_SPECIAL=false
```

- `PASSWORD_MIN_LENGTH` (optional): Minimum password length
  - Default: `8`

- `PASSWORD_MAX_LENGTH` (optional): Maximum password length
  - Default: `128`

- `PASSWORD_REQUIRE_UPPERCASE` (optional): Require uppercase letters
  - Default: `true`

- `PASSWORD_REQUIRE_LOWERCASE` (optional): Require lowercase letters
  - Default: `true`

- `PASSWORD_REQUIRE_NUMBER` (optional): Require numbers
  - Default: `true`

- `PASSWORD_REQUIRE_SPECIAL` (optional): Require special characters
  - Default: `false`

## Environment Variables

### Satellite Environment Variables

The satellite can be configured using environment variables or CLI flags:

- `TOKEN` (required): Satellite token for authentication with Ground Control
- `GROUND_CONTROL_URL` (required): URL of the Ground Control service
- `USE_UNSECURE` (optional): Use insecure connections (set to `"true"` to enable)

### Docker Compose Environment Variables

When using Docker Compose, you can set these in your `.env` file or `docker-compose.yml`:

```yaml
environment:
  - GROUND_CONTROL_URL=${GROUND_CONTROL_URL}
  - TOKEN=${TOKEN}
  - USE_UNSECURE=${USE_UNSECURE:-false}
```

## CLI Flags

The satellite binary supports the following CLI flags:

- `--token` (string): Satellite token for authentication
  - Can also be set via `TOKEN` environment variable

- `--ground-control-url` (string): URL of the Ground Control service
  - Can also be set via `GROUND_CONTROL_URL` environment variable

- `--use-unsecure` (boolean): Use insecure (HTTP) connections
  - Default: `false`
  - Can also be set via `USE_UNSECURE` environment variable

- `--json-logging` (boolean): Enable JSON logging
  - Default: `true`

- `--mirrors` (string, repeatable): Configure CRI mirrors
  - Format: `CRI:registry1,registry2`
  - Examples:
    - `--mirrors=containerd:docker.io,quay.io`
    - `--mirrors=podman:docker.io`
    - `--mirrors=docker:true` (for Docker, only docker.io is supported)

## Configuration Examples

### Minimal Configuration

```json
{
  "app_config": {
    "ground_control_url": "http://127.0.0.1:9090"
  }
}
```

### Production Configuration

```json
{
  "app_config": {
    "ground_control_url": "https://gc.example.com",
    "log_level": "info",
    "use_unsecure": false,
    "state_replication_interval": "@every 00h05m00s",
    "register_satellite_interval": "@every 00h01m00s",
    "heartbeat_interval": "@every 00h05m00s",
    "local_registry": {
      "url": "http://127.0.0.1:8585"
    },
    "metrics": {
      "collect_cpu": true,
      "collect_memory": true,
      "collect_storage": true
    }
  },
  "zot_config": {
    "distSpecVersion": "1.1.0",
    "storage": {
      "rootDirectory": "/var/lib/harbor-satellite/zot"
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
```

### Using External Registry

```json
{
  "app_config": {
    "ground_control_url": "http://127.0.0.1:9090",
    "bring_own_registry": true,
    "local_registry": {
      "url": "http://my-registry:5000",
      "username": "admin",
      "password": "password"
    }
  }
}
```

### Development Configuration

```json
{
  "app_config": {
    "ground_control_url": "http://127.0.0.1:9090",
    "log_level": "debug",
    "use_unsecure": true,
    "state_replication_interval": "@every 00h00m10s",
    "register_satellite_interval": "@every 00h00m05s",
    "heartbeat_interval": "@every 00h00m30s"
  },
  "zot_config": {
    "distSpecVersion": "1.1.0",
    "storage": {
      "rootDirectory": "./zot"
    },
    "http": {
      "address": "127.0.0.1",
      "port": "8585"
    },
    "log": {
      "level": "debug"
    }
  }
}
```

## Configuration Validation

The satellite validates configuration on startup and applies defaults where necessary. Invalid configurations will result in startup errors. Common validation issues:

- Invalid URLs: URLs must be valid HTTP/HTTPS URLs
- Invalid cron expressions: Cron expressions must be valid
- Invalid log levels: Log levels must be one of the valid values
- Missing required fields: Required fields must be provided or have defaults

## Hot Reload

The satellite supports hot-reloading configuration changes. When the configuration file is modified, the satellite will:

1. Validate the new configuration
2. Apply changes without restarting
3. Log warnings for any defaulted or ignored fields

To trigger a hot reload, simply update the configuration file. The satellite watches the file for changes.

## Related Documentation

- [Getting Started Guide](getting-started.md) - Initial setup instructions
- [API Reference](api-reference.md) - Ground Control API for managing configurations
- [Architecture Documentation](architecture/overview.md) - System architecture
