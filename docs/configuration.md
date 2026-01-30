# Configuration Reference

This document provides a comprehensive reference for all Harbor Satellite configuration options, including Ground Control and Satellite configurations.

## Ground Control Configuration

Ground Control is configured via environment variables in the `.env` file.

### Harbor Registry Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `HARBOR_URL` | URL of the Harbor registry instance | - | Yes |
| `HARBOR_USERNAME` | Harbor admin username | - | Yes |
| `HARBOR_PASSWORD` | Harbor admin password | - | Yes |

### Ground Control Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `PORT` | Port for Ground Control to listen on | 9090 | No |
| `ADMIN_PASSWORD` | Password for the admin user | - | Yes |
| `SESSION_DURATION_HOURS` | Session token validity duration | 24 | No |
| `LOCKOUT_DURATION_MINUTES` | Account lockout duration after failed attempts | 15 | No |

### Password Policy Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `PASSWORD_MIN_LENGTH` | Minimum password length | 8 | No |
| `PASSWORD_MAX_LENGTH` | Maximum password length | 128 | No |
| `PASSWORD_REQUIRE_UPPERCASE` | Require uppercase letters | true | No |
| `PASSWORD_REQUIRE_LOWERCASE` | Require lowercase letters | true | No |
| `PASSWORD_REQUIRE_NUMBER` | Require numbers | true | No |
| `PASSWORD_REQUIRE_SPECIAL` | Require special characters | false | No |

### Database Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `DB_HOST` | Database host | 127.0.0.1 | No |
| `DB_PORT` | Database port | 5432 | No |
| `DB_DATABASE` | Database name | groundcontrol | No |
| `DB_USERNAME` | Database username | postgres | No |
| `DB_PASSWORD` | Database password | password | No |

## Satellite Configuration

Satellite configuration is managed through Ground Control and stored as JSON configuration objects.

### App Configuration (`app_config`)

| Field | Type | Description | Default | Required |
|-------|------|-------------|---------|----------|
| `ground_control_url` | string | URL of Ground Control service | - | Yes |
| `log_level` | string | Logging level (debug, info, warn, error) | "info" | No |
| `use_unsecure` | boolean | Allow insecure HTTPS connections | false | No |
| `state_replication_interval` | string | Cron expression for state replication | "@every 00h00m10s" | No |
| `register_satellite_interval` | string | Cron expression for satellite registration | "@every 00h00m10s" | No |
| `bring_own_registry` | boolean | Use external registry instead of Zot | false | No |
| `disable_heartbeat` | boolean | Disable heartbeat to Ground Control | false | No |
| `heartbeat_interval` | string | Cron expression for heartbeat | "@every 00h00m30s" | No |

### Local Registry Configuration

| Field | Type | Description | Default | Required |
|-------|------|-------------|---------|----------|
| `url` | string | URL of local registry | "http://0.0.0.0:8585" | No |
| `username` | string | Registry username | - | No |
| `password` | string | Registry password | - | No |

### Metrics Configuration

| Field | Type | Description | Default | Required |
|-------|------|-------------|---------|----------|
| `collect_cpu` | boolean | Collect CPU metrics | false | No |
| `collect_memory` | boolean | Collect memory metrics | false | No |
| `collect_storage` | boolean | Collect storage metrics | false | No |

### State Configuration (`state_config`)

| Field | Type | Description | Default | Required |
|-------|------|-------------|---------|----------|
| `auth.url` | string | Upstream registry URL | - | Yes |
| `auth.username` | string | Upstream registry username | - | Yes |
| `auth.password` | string | Upstream registry password | - | Yes |
| `state` | string | State endpoint URL | - | No |

### Zot Registry Configuration (`zot_config`)

Zot configuration follows the [Zot registry configuration format](https://zotregistry.io/v2.0.0/admin-guide/admin-configuration/). Key fields include:

| Field | Type | Description | Default | Required |
|-------|------|-------------|---------|----------|
| `distSpecVersion` | string | Distribution spec version | "1.1.0" | No |
| `storage.rootDirectory` | string | Storage directory path | "./zot" | No |
| `http.address` | string | HTTP listen address | "0.0.0.0" | No |
| `http.port` | string | HTTP listen port | "8585" | No |
| `log.level` | string | Log level | "info" | No |

## Example Configurations

### Minimal Satellite Configuration

```json
{
  "state_config": {
    "auth": {
      "url": "https://harbor.example.com",
      "username": "robot$account",
      "password": "token"
    }
  },
  "app_config": {
    "ground_control_url": "http://ground-control:9090",
    "log_level": "info",
    "use_unsecure": false,
    "state_replication_interval": "@every 00h00m10s",
    "register_satellite_interval": "@every 00h00m10s",
    "local_registry": {
      "url": "http://localhost:5000"
    },
    "heartbeat_interval": "@every 00h00m30s"
  },
  "zot_config": {
    "distSpecVersion": "1.1.0",
    "storage": {
      "rootDirectory": "/var/lib/zot"
    },
    "http": {
      "address": "0.0.0.0",
      "port": "5000"
    },
    "log": {
      "level": "info"
    }
  }
}
```

### Advanced Configuration with Metrics

```json
{
  "state_config": {
    "auth": {
      "url": "https://harbor.example.com",
      "username": "robot$account",
      "password": "token"
    }
  },
  "app_config": {
    "ground_control_url": "http://ground-control:9090",
    "log_level": "debug",
    "use_unsecure": false,
    "state_replication_interval": "@every 00h01m00s",
    "register_satellite_interval": "@every 00h00m30s",
    "local_registry": {
      "url": "http://localhost:5000",
      "username": "admin",
      "password": "secure-password"
    },
    "heartbeat_interval": "@every 00h00m30s",
    "metrics": {
      "collect_cpu": true,
      "collect_memory": true,
      "collect_storage": true
    }
  },
  "zot_config": {
    "distSpecVersion": "1.1.0",
    "storage": {
      "rootDirectory": "/var/lib/zot",
      "gc": true,
      "dedupe": true
    },
    "http": {
      "address": "0.0.0.0",
      "port": "5000",
      "tls": {
        "cert": "/etc/ssl/certs/zot.crt",
        "key": "/etc/ssl/private/zot.key"
      }
    },
    "log": {
      "level": "debug",
      "output": "/var/log/zot/zot.log"
    },
    "extensions": {
      "search": {
        "enable": true
      },
      "metrics": {
        "enable": true,
        "prometheus": {
          "path": "/metrics"
        }
      }
    }
  }
}
```

## Cron Expression Format

Harbor Satellite uses cron expressions for scheduling tasks. The format follows the [cron package](https://pkg.go.dev/github.com/robfig/cron) specification:

- `@every <duration>` - Run every specified duration
- Standard cron format: `minute hour day month weekday`

Examples:
- `@every 00h00m10s` - Every 10 seconds
- `@every 00h01m00s` - Every 1 minute
- `@every 01h00m00s` - Every 1 hour
- `0 0 * * *` - Every day at midnight
- `0 */6 * * *` - Every 6 hours

## Environment Variables for Satellite

When running Satellite directly (not through Docker), additional environment variables can be set:

| Variable | Description | Default |
|----------|-------------|---------|
| `SATELLITE_TOKEN` | Satellite authentication token | - |
| `GROUND_CONTROL_URL` | Ground Control URL | - |
| `LOG_LEVEL` | Logging level | info |
| `JSON_LOGGING` | Enable JSON logging | true |

## Configuration Validation

Satellite validates configuration on startup. Common validation errors:

- Invalid URLs
- Missing required authentication fields
- Invalid cron expressions
- Incompatible registry configurations

Check logs for detailed validation error messages.</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/configuration.md