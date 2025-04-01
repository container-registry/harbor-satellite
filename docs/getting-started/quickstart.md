# Quick Start Guide

This guide provides the fastest way to get started with Harbor Satellite.

## Prerequisites

Ensure you have:
- A Harbor registry instance (or similar OCI-compliant registry)
- Credentials with permission to create robot accounts in the registry
- The latest version of Dagger installed

## Quick Setup

### 1. Configure Ground Control

Navigate to the `ground-control` directory and set up the environment variables:

```bash
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=https://demo.goharbor.io

PORT=8080
APP_ENV=local

DB_HOST=127.0.0.1 # For Dagger use DB_HOST=pgservice
DB_PORT=8100 # For Dagger use DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres       
DB_PASSWORD=password  
```

### 2. Run Ground Control

#### Option 1: Using Dagger (Recommended)

To start the Ground Control service, execute the following Dagger command:

```bash
dagger call run-ground-control up
```

You can also build ground-control binary using:

```bash
dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev
```

To Run ground-control binary:

```bash
./gc-dev
```

#### Option 2: Without Dagger

1. First, move to the ground-control directory:
```bash
cd ground-control
```

2. Add the credentials to the `docker-compose` file for the Postgres service and pgAdmin, and start the services. Make sure you add the same credentials that you have added in the .env file:
```bash
docker compose up
```

3. Once the services are up, move to the `sql/schema` folder to set up the database:
```bash
cd sql/schema
```

4. Install `goose` to run database migrations if not already installed:
```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

5. Run the database migrations with your credentials:
```bash
goose postgres "postgres://<POSTGRES_USER>:<POSTGRES_PASSWORD>@localhost:8100/groundcontrol?sslmode=disable" up
```

6. Start Ground Control:
```bash
cd ../..
go run main.go
```

> **Note:** Ensure you have set up Dagger with the latest version before running this command. Ground Control will run on port 8080.

### 3. Register the Satellite with Ground Control

First, check the health of the server:

```bash
curl --location 'http://localhost:8080/health'
```

Create a group:
> **Note:** Please modify the body given below according to your registry
```bash
curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
--data '{
  "group": "GROUP_NAME",
  "registry": "YOUR_REGISTRY_URL",
  "artifacts": [
    {
      "repository": "YOUR_PROJECT/YOUR_IMAGE",
      "tag": ["TAGS OF THE IMAGE"],
      "type": TYPE_OF_IMAGE,
      "digest": "DIGEST",
      "deleted": false
    }
  ]
}'
```

Example Curl Command for creating `GROUP`:
```bash
curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
--data '{
  "group": "group1",
  "registry": "https://demo.goharbor.io",
  "artifacts": [
    {
      "repository": "alpine/alpine",
      "tag": ["latest"],
      "type": "docker",
      "digest": "sha256:3e21c52835bab96cbecb471e3c3eb0e8a012b91ba2f0b934bd0b5394cd570b9f",
      "deleted": false
    }
  ]
}'
```

Register a satellite:
```bash
curl --location 'http://localhost:8080/satellites/register' \
--header 'Content-Type: application/json' \
--data '{
    "name": "SATELLITE_NAME",
    "groups": ["GROUP_NAME"]
}'
```

> **Note**: Running the above command will produce a token which is important for the satellite to register itself to the ground control.
> **Note**: The `satellitename` and `groupname` must be 1-255 characters, start with a letter or number, and contain only lowercase letters, numbers, and ._-"

### 4. Configure Satellite

Create a `config.json` file with the following content:

```json
{
  "environment_variables": {
    "ground_control_url": "http://127.0.0.1:8080", // URL for the ground control server
    "log_level": "info", // Log level: can be "debug", "info", "warn", or "error"
    "use_unsecure": true, // Use unsecure connections (set to true for dev environments)
    "zot_config_path": "./registry/config.json", // Path to Zot registry configuration file
    "token": "ADD_THE_TOKEN_FROM_THE_ABOVE_STEP", // add the token received while registering satellite
    "jobs": [
      // List of scheduled jobs
      // Checkout https://pkg.go.dev/github.com/robfig/cron#hdr-Predefined_schedules for more
      //  details on how to write the cron job config
      {
        "name": "replicate_state", // Job to replicate state
        "schedule": "@every 00h00m10s" // Schedule interval: every 10 seconds
      },
      {
        "name": "update_config", // Job to update configuration
        "schedule": "@every 00h00m30s" // Schedule interval: every 30 seconds
      },
      {
        "name": "register_satellite", // Job to register satellite
        "schedule": "@every 00h00m05s" // Schedule interval: every 5 seconds
      }
    ],
    "local_registry": {
      // Configuration for the local registry
      "url": "", // Add your own registry URL if bring_own_registry is true else leave blank
      "username": "", // Add your own registry username if bring_own_registry is true else leave blank
      "password": "", // Add your own registry password if bring_own_registry is true else leave blank
      "bring_own_registry": false // Set to true if using an external registry and the above config
    }
  }
}
```

### 5. Start Satellite

You can start the satellite in two ways:

1. Run directly:
```bash
go run main.go
```

2. Build and run the satellite:
```bash
dagger call build --source=. --component=satellite export --path=./bin
```
> **Note**: This would generate the binaries for various architectures in the `bin` folder. Choose the binary for your system and use it. Make sure that the `config.json` and the binary directory are the same when running it otherwise it would throw an error.

## Troubleshooting

### Common Issues

1. **Ground Control Connection Issues**
   - Verify the Ground Control URL in config.json
   - Check if Ground Control is running
   - Ensure network connectivity

2. **Registry Access Issues**
   - Verify Harbor credentials
   - Check network connectivity to Harbor
   - Ensure proper permissions

3. **Container Runtime Integration**
   - Verify container runtime configuration
   - Check network connectivity
   - Ensure proper permissions

### Logs

- Ground Control logs: `docker compose logs`
- Satellite logs: `docker logs <container_id>`

## Next Steps

For more detailed information, see:
- [User Guide](../user-guide/README.md) - For detailed usage instructions
- [Architecture Guide](../architecture/README.md) - For system design details