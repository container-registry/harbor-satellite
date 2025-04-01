# Configuration Guide

This guide explains how to configure the Harbor Satellite system, including all components and their settings.

## Configuration Overview

Harbor Satellite uses JSON configuration files for its components. The main configuration files are:

1. `config.json` - Main Satellite configuration
2. `registry/config.json` - Registry configuration
3. `.env` - Environment variables for Ground Control

## Satellite Configuration

### Main Configuration File

Create a `config.json` file in the root directory:

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
    ],
    "local_registry": {
      "url": "",
      "username": "",
      "password": "",
      "bring_own_registry": false
    }
  }
}
```

### Configuration Options

#### Basic Settings
- `ground_control_url`: URL of the Ground Control server
- `log_level`: Logging level (debug, info, warn, error)
- `use_unsecure`: Whether to use unsecure connections
- `zot_config_path`: Path to registry configuration file
- `token`: Authentication token for Ground Control

#### Jobs Configuration
- `name`: Job identifier
- `schedule`: Cron-style schedule

#### Local Registry Settings
- `url`: Custom registry URL
- `username`: Registry username
- `password`: Registry password
- `bring_own_registry`: Use external registry

## Registry Configuration

### Registry Configuration File

Create a `registry/config.json` file:

```json
{
  "storage": {
    "rootDirectory": "/var/lib/registry",
    "maxSize": "100GB",
    "cache": {
      "enabled": true,
      "maxSize": "10GB"
    }
  },
  "http": {
    "addr": ":5000",
    "tls": {
      "enabled": true,
      "cert": "/path/to/cert",
      "key": "/path/to/key"
    }
  },
  "log": {
    "level": "info",
    "format": "text"
  }
}
```

### Configuration Options

#### Storage Settings
- `rootDirectory`: Registry storage location
- `maxSize`: Maximum storage size
- `cache`: Cache configuration

#### HTTP Settings
- `addr`: Listen address
- `tls`: TLS configuration

#### Logging Settings
- `level`: Log level
- `format`: Log format

## Ground Control Configuration

### Environment Variables

Create a `.env` file in the `ground-control` directory:

```bash
# Harbor Registry Configuration
HARBOR_USERNAME=admin
HARBOR_PASSWORD=your_password
HARBOR_URL=your_harbor_url

# Application Configuration
PORT=8080
APP_ENV=local

# Database Configuration
DB_HOST=localhost
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres
DB_PASSWORD=your_db_password
```

### Configuration Options

#### Harbor Settings
- `HARBOR_USERNAME`: Harbor registry username
- `HARBOR_PASSWORD`: Harbor registry password
- `HARBOR_URL`: Harbor registry URL

#### Application Settings
- `PORT`: Server port
- `APP_ENV`: Environment (local, production)

#### Database Settings
- `DB_HOST`: Database host
- `DB_PORT`: Database port
- `DB_DATABASE`: Database name
- `DB_USERNAME`: Database username
- `DB_PASSWORD`: Database password

## Security Configuration

### TLS Configuration

1. Generate certificates:
```bash
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout private.key -out certificate.crt
```

2. Configure TLS in registry:
```json
{
  "http": {
    "tls": {
      "enabled": true,
      "cert": "/path/to/certificate.crt",
      "key": "/path/to/private.key"
    }
  }
}
```

### Authentication Configuration

1. Generate token:
```bash
curl -X POST http://localhost:8080/satellites/register \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-satellite",
    "groups": ["my-group"]
  }'
```

2. Configure token in satellite:
```json
{
  "environment_variables": {
    "token": "your_generated_token"
  }
}
```

## Performance Configuration

### Cache Settings

```json
{
  "storage": {
    "cache": {
      "enabled": true,
      "maxSize": "10GB",
      "ttl": "24h"
    }
  }
}
```

### Resource Limits

```json
{
  "storage": {
    "maxSize": "100GB",
    "maxConcurrentUploads": 10,
    "maxConcurrentDownloads": 10
  }
}
```

## Monitoring Configuration

### Health Check Settings

```json
{
  "health": {
    "enabled": true,
    "interval": "30s",
    "timeout": "5s"
  }
}
```

### Metrics Settings

```json
{
  "metrics": {
    "enabled": true,
    "port": 9090,
    "path": "/metrics"
  }
}
```

## Troubleshooting

### Common Configuration Issues

1. **Connection Issues**
   - Verify URLs and ports
   - Check network connectivity
   - Validate credentials

2. **Storage Issues**
   - Check storage permissions
   - Verify disk space
   - Validate paths

3. **Authentication Issues**
   - Verify tokens
   - Check credentials
   - Validate certificates

### Configuration Validation

```bash
# Validate satellite configuration
satellite validate-config config.json

# Validate registry configuration
registry validate-config registry/config.json
```

## Best Practices

1. **Security**
   - Use secure connections
   - Implement proper authentication
   - Regular token rotation
   - Secure storage

2. **Performance**
   - Optimize cache settings
   - Configure resource limits
   - Monitor usage
   - Regular cleanup

3. **Reliability**
   - Regular backups
   - Health monitoring
   - Error logging
   - Recovery procedures
