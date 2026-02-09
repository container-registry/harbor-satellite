# Configuration Reference

This guide covers all configuration options for Harbor Satellite components.

## Configuration Files

Harbor Satellite uses JSON configuration files:

- **Satellite Configuration** — `/config/config.json`
- **Ground Control Configuration** — Environment variables and command-line flags

## Satellite Configuration

Satellite configuration consists of three main sections:

### Configuration Structure

```json
{
  "state_config": {
    "auth": {},
    "state": "string"
  },
  "app_config": {
    "ground_control_url": "string",
    "log_level": "string",
    "use_unsecure": "boolean",
    "state_replication_interval": "string",
    "update_config_interval": "string", 
    "register_satellite_interval": "string",
    "heartbeat_interval": "string",
    "metrics": {},
    "local_registry": {},
    "tls": {},
    "encrypt_config": "boolean"
  },
  "zot_config": {}
}
```

### State Configuration

Controls how satellites authenticate and manage state synchronization.

#### `state_config.auth`

Harbor authentication credentials (managed by Ground Control):

```json
{
  "auth": {
    "url": "https://harbor.example.com",
    "username": "robot_account_name", 
    "password": "robot_account_token"
  }
}
```

#### `state_config.state`

State artifact reference for satellite configuration:

```json
{
  "state": "harbor.example.com/satellite/satellite-state/my-satellite/state:latest"
}
```

### Application Configuration

Core satellite runtime settings.

#### Ground Control Connection

- **`ground_control_url`** (required) — Ground Control service URL
  - Example: `"http://ground-control:8080"`

- **`use_unsecure`** (boolean) — Disable TLS verification for Ground Control connections  
  - Default: `false`
  - Example: `true` (for development only)

#### Intervals and Timing

All intervals use cron syntax (`@every NNhNNmNNs`):

- **`state_replication_interval`** — How often to sync state from Ground Control
  - Default: `"@every 00h00m10s"`

- **`update_config_interval`** — How often to check for configuration updates
  - Default: `"@every 00h00m10s"`

- **`register_satellite_interval`** — How often to re-register with Ground Control  
  - Default: `"@every 00h00m10s"`

- **`heartbeat_interval`** — Heartbeat frequency to Ground Control
  - Default: `"@every 00h00m30s"`

#### Logging

- **`log_level`** — Log verbosity level
  - Options: `"trace"`, `"debug"`, `"info"`, `"warn"`, `"error"`
  - Default: `"info"`

#### Metrics Collection

```json
{
  "metrics": {
    "collect_cpu": true,
    "collect_memory": true, 
    "collect_storage": true
  }
}
```

- **`collect_cpu`** — Enable CPU usage metrics
- **`collect_memory`** — Enable memory usage metrics  
- **`collect_storage`** — Enable storage usage metrics

#### Local Registry Settings

```json
{
  "local_registry": {
    "url": "http://0.0.0.0:8585"
  }
}
```

- **`url`** — Local registry endpoint URL

#### TLS Configuration

```json
{
  "tls": {
    "cert_file": "/path/to/cert.pem",
    "key_file": "/path/to/key.pem", 
    "ca_file": "/path/to/ca.pem",
    "skip_verify": false
  }
}
```

- **`cert_file`** — TLS certificate file path
- **`key_file`** — TLS private key file path
- **`ca_file`** — CA certificate file path
- **`skip_verify`** — Skip TLS certificate verification (development only)

#### Security

- **`encrypt_config`** (boolean) — Encrypt sensitive configuration data
  - Default: `false`

### Zot Registry Configuration

Embedded [Zot](https://zotregistry.io) registry settings.

#### Basic Configuration

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

#### Storage Configuration

- **`storage.rootDirectory`** — Directory for image storage
- **`storage.dedupe`** — Enable layer deduplication
- **`storage.gc`** — Garbage collection settings

#### HTTP Server  

- **`http.address`** — Bind address for registry server
- **`http.port`** — Port for registry server
- **`http.tls`** — TLS configuration for registry endpoints

## Ground Control Configuration

Ground Control is configured via environment variables.

### Database Configuration

Required for Ground Control operation:

```bash
DB_HOST=localhost
DB_PORT=5432
DB_DATABASE=satellite  
DB_USERNAME=satellite
DB_PASSWORD=your-password
DB_SSLMODE=disable
```

### Harbor Integration

```bash
HARBOR_URL=https://harbor.example.com
HARBOR_TOKEN=your-robot-token
```

### Server Configuration

```bash
# HTTP server settings
GC_HOST=0.0.0.0
GC_PORT=8080

# Authentication
GC_SECRET=your-jwt-secret
GC_TOKEN_TTL=24h

# Logging
LOG_LEVEL=info
```

### SPIFFE/SPIRE Integration

For SPIFFE-based authentication:

```bash
# SPIRE server settings  
SPIFE_ENABLED=true
SPIRE_SOCKET_PATH=/tmp/spire-server/api.sock
SPIRE_TRUST_DOMAIN=example.org
```

## Example Configurations

### Development Environment

Minimal configuration for local development:

```json
{
  "state_config": {
    "auth": {}
  },
  "app_config": {
    "ground_control_url": "http://localhost:8080",
    "log_level": "debug",
    "use_unsecure": true,
    "local_registry": {
      "url": "http://127.0.0.1:8585"  
    }
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

### Production Environment

Secure configuration with TLS and proper intervals:

```json
{
  "state_config": {
    "auth": {}
  },
  "app_config": {
    "ground_control_url": "https://ground-control.company.com",
    "log_level": "info",
    "use_unsecure": false,
    "state_replication_interval": "@every 00h05m00s",
    "update_config_interval": "@every 00h15m00s",
    "register_satellite_interval": "@every 01h00m00s", 
    "heartbeat_interval": "@every 00h01m00s",
    "metrics": {
      "collect_cpu": true,
      "collect_memory": true,
      "collect_storage": true
    },
    "local_registry": {
      "url": "https://satellite.edge.company.com"
    },
    "tls": {
      "cert_file": "/etc/ssl/certs/satellite.pem",
      "key_file": "/etc/ssl/private/satellite.key",
      "ca_file": "/etc/ssl/certs/ca.pem",
      "skip_verify": false
    },
    "encrypt_config": true
  },
  "zot_config": {
    "distSpecVersion": "1.1.0",
    "storage": {
      "rootDirectory": "/var/lib/satellite/zot",
      "dedupe": true,
      "gc": true
    },
    "http": {
      "address": "0.0.0.0",
      "port": "8585",
      "tls": {
        "cert": "/etc/ssl/certs/registry.pem",
        "key": "/etc/ssl/private/registry.key"
      }
    },
    "log": {
      "level": "warn"
    }
  }
}
```

### High-Availability Edge

Configuration for edge locations with intermittent connectivity:

```json
{
  "app_config": {
    "ground_control_url": "https://ground-control.company.com",
    "log_level": "info",
    "use_unsecure": false,
    "state_replication_interval": "@every 00h01m00s",
    "update_config_interval": "@every 00h05m00s", 
    "register_satellite_interval": "@every 00h30m00s",
    "heartbeat_interval": "@every 00h00m30s",
    "local_registry": {
      "url": "http://0.0.0.0:8585"
    }
  }
}
```

## Configuration Management

### Environment Variables

Satellite configuration can be partially overridden with environment variables:

```bash
# Override Ground Control URL
SATELLITE_GROUND_CONTROL_URL=https://backup-gc.company.com

# Override log level
SATELLITE_LOG_LEVEL=debug

# Override registry URL  
SATELLITE_REGISTRY_URL=http://localhost:5000
```

### Configuration Updates

Satellites automatically refresh configuration from Ground Control based on `update_config_interval`. Configuration changes are applied without restart when possible.

### Validation

Use the satellite binary to validate configuration:

```bash
# Validate configuration file
./satellite validate-config --config /path/to/config.json

# Test Ground Control connectivity
./satellite test-connection --config /path/to/config.json
```

## Security Considerations

- Store sensitive values like tokens and passwords in environment variables or secret management systems
- Use TLS for all production communications
- Enable configuration encryption for sensitive data at rest
- Regularly rotate authentication credentials
- Monitor configuration changes and access patterns

## Troubleshooting

- **Configuration validation errors** — Check JSON syntax and required fields
- **Connection failures** — Verify network connectivity and TLS settings
- **Authentication issues** — Validate Harbor credentials and Ground Control registration
- **Performance issues** — Adjust interval timing based on network conditions

See the [Troubleshooting Guide](troubleshooting.md) for detailed solutions.