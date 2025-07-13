# Quick Start Guide

This guide provides the fastest way to get started with Harbor Satellite.

## Prerequisites

Ensure you have:
- A Harbor registry instance (or similar OCI-compliant registry)
- Credentials with permission to create robot accounts in the registry
- The latest version of Dagger installed

## Quick Setup

### 1. Configure Ground Control

Navigate to the `ground-control` directory and set up the environment variables like this : 

```bash
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=https://demo.goharbor.io

PORT=8080
APP_ENV=local

DB_HOST=127.0.0.1 # For Dagger use DB_HOST=pgservice
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres       
DB_PASSWORD=password  
```
You can also directly edit this [example](https://github.com/meethereum/harbor-satellite/blob/update-docs/ground-control/.env.example) available in the repository.

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

2. Add the credentials to the `docker-compose` file for all the services needed for ground control. Make sure you add the same credentials that you have added in the .env file. Some additional tweaks can be also made, since this uses pre-built images.
```bash
docker compose up
```

> **Note:** Ensure you have set up Dagger with the latest version before running this command. Ground Control will run on port 8080.

### 3. Create a group for the artifacts.

First, check the health of the ground control:

```bash
curl --location 'http://localhost:8080/health'
```

A group is just a set of images that the satellite needs to replicate. Alternatively, you can also use the groups already present in the upstream testing registry and skip this step entirely.

A group is just a set of images that the satellite needs to replicate from the upstream registry.It also consists information about all the artifacts present in it. 
> Upstream registry is the remote registry from which the satellite component pulls all the artifacts from and pushes them to the local OCI-compliant registry. 

Alternatively, you can also use the groups already present in the upstream testing registry and skip this step entirely.

**Todo** : Guides users how to access this group

> **Note:** You must modify the body given below according to your registry. 
```bash
curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
--data '{
  "group": "group1", # name of the group
  "registry": "http://demo.goharbor.io", # your registry URL here
  "artifacts": [
    {
      # your artifact information here
      "repository": "satellite/alpine",
      "tag": ["latest"],
      "type": "docker",
      "digest": "sha256:5a6ee6c36824d527a0fe91a2a7c160c2e286bbeae46cd931c337ac769f1bd930",
      "deleted": false
    }
  ]
}
'
```

### 4. Configure  the satellite

Now you need to create a config artifact for the satellite.
This artifact tells the satellite where the ground control is located and defines how and when to replicate artifacts from it. It also includes details about the local OCI-compliant registry, specified separately under its own field.

```bash
curl --location 'http://localhost:8080/configs' \
--header 'Content-Type: application/json' \
--data '{
  "config_name": "config1",
  "registry": "http://demo.goharbor.io", # your registry URL here
  "config":
{ 
    "state_config": {},
    "app_config": {
        "ground_control_url": "http://127.0.0.1:8080",
        "log_level": "info",
        "use_unsecure": true,
        "zot_config_path": "./registry_config.json",
        "state_replication_interval": "@every 00h00m10s",
        "update_config_interval": "@every 00h00m10s",
        "register_satellite_interval": "@every 00h00m10s",
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
            "level": "info"
        }
      }
}
}
'
```

### 5. Register satellite

You now need to use the group and config you created to create the satellite. A successful request returns a token which needs to be preserved for future steps.

```bash
curl --location 'http://localhost:8080/satellites' \
--header 'Content-Type: application/json' \
--data '{
    "name": "satellite_1",
    "groups": ["group1"],  # name of the group you created
    "config_name": "config1" # name of the config you created
}'
```

### 6. Start the satellite
Setup .env files. An [example](https://github.com/container-registry/harbor-satellite/blob/main/.env.example) is given here. The token you received earlier will be used here.

You can directly run : 

```bash
go run cmd/main.go --token "<your token here>" --ground-control-url "<ground control url here>"
```
> Note : by default, logging in JSON format is set to true. To change this pass additional flag `--json-logging=false`

## Troubleshooting

### Common Issues

1. **Ground Control Connection Issues**
   - Verify the Ground Control URL in config.json
   - Check if Ground Control is running
   - Cross check the environment variables and the Docker Compose file

2. **Registry Access Issues**
   - Verify Harbor credentials
   - Check network connectivity to Harbor
   - Ensure proper permissions

