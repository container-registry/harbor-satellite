# Harbor Satellite Configuration Guide

This guide provides a complete reference for all configuration options in Harbor Satellite, explaining both the structure and practical usage of the configuration system.

## Configuration Overview

Harbor Satellite uses a JSON configuration file (`config.json`) for its settings. The configuration is divided into two main sections:

1. `state_config` - Configuration for state management
2. `environment_variables` - Main satellite configuration

## Complete Configuration Example

```json
{
  "state_config": {
    "auth": {
      "name": "",        // Source registry username
      "registry": "",    // Source registry URL
      "secret": ""       // Source registry password
    },
    "states": [""]      // List of states to manage
  },
  "environment_variables": {
    "ground_control_url": "http://127.0.0.1:8080",  // Ground Control server URL
    "log_level": "info",                            // Log level (debug, info, warn, error)
    "use_unsecure": true,                           // Use unsecure connections
    "zot_config_path": "./registry/config.json",    // Path to Zot registry config
    "token": "",                                    // Authentication token
    "state_replication_interval": "@every 00h00m10s", // // Job to replicate state
    "update_config_interval": "@every 00h00m10s", // Job to update configuration  
    "register_satellite_interval": "@every 00h00m10s", // Job to register satellite
    "local_registry": {
      "url": "",                                   // Custom registry URL
      "username": "",                              // Registry username
      "password": "",                              // Registry password
      "bring_own_registry": false                  // Use external registry
    }
  }
}
```

## Configuration Options

### State Configuration

- `state_config.auth`: Authentication for source registry
  - `name`: Username for source registry
  - `registry`: URL of source registry
  - `secret`: Password for source registry
- `states`: List of states to manage

### Environment Variables

- `ground_control_url`: URL of the Ground Control server
- `log_level`: Logging level (debug, info, warn, error)
- `use_unsecure`: Enable/disable secure connections
- `zot_config_path`: Path to Zot registry configuration
- `token`: Authentication token for Ground Control
- `replicate_state`: Synchronizes state with Ground Control
- `update_config`: Updates satellite configuration
- `register_satellite`: Handles satellite registration


### Local Registry Configuration

- `url`: Custom registry URL (if using external registry)
- `username`: Registry username
- `password`: Registry password
- `bring_own_registry`: Enable external registry usage

## Zot Registry Configuration

The local registry uses Zot with the following configuration:

```json
{
  "distSpecVersion": "1.1.0",
  "storage": {
    "rootDirectory": "./zot"
  },
  "http": {
    "address": "127.0.0.1",
    "port": "8585"
  },
  "log": {
    "level": "info"
  }
}
```

## Container Runtime Integration

Harbor Satellite supports integration with:
- containerd
- CRI-O

Configuration for these runtimes is handled automatically by the satellite based on the main configuration.
