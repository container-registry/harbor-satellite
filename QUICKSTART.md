# Quick Start Guide

This guide provides the fastest way to get started with Harbor Satellite.

## Prerequisites

Ensure you have:
- A Harbor registry instance with satellite adapter. This should be available [here](https://github.com/container-registry/harbor-next/tree/satellite)
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

DB_HOST=127.0.0.1 # For Dagger use DB_HOST=pgservice
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres       
DB_PASSWORD=password  
```
You can also directly edit this [example](ground-control/.env.example) available in the repository.

### 2. Run Ground Control

#### Option 1: Without Dagger(recommended for end users)

1. First, move to the ground-control directory:
```bash
cd ground-control
```

2. Add the credentials to the `docker-compose` file for all the services needed for ground control. Make sure you add the same credentials that you have added in the .env file. Some additional tweaks can be also made, since this uses pre-built images.
```bash
docker compose up
```

#### Option 2: build the binary(recommended for end users)
You can also build ground-control binary using:

```bash
dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev
```

To Run ground-control binary:

```bash
./gc-dev
```

#### Option 3: Using Dagger (recommended for developers)

To start the Ground Control service, execute the following Dagger command:

```bash
dagger call run-ground-control up
```


### 3. Create a group for the artifacts.

First, check the health of the ground control:

```bash
curl --location 'http://localhost:8080/health'
```

A group is just a set of images that the satellite needs to replicate from the upstream registry.It also consists information about all the artifacts present in it. 
> Upstream registry is the remote registry from which the satellite component pulls all the artifacts from and pushes them to the local OCI-compliant registry. 



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
      "repository": "library/alpine",
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

Now you need to create a config artifact for the satellite. An example is given [here](examples/config.json).
This artifact tells the satellite where the ground control is located and defines how and when to replicate artifacts from it. It also includes details about the local OCI-compliant registry, specified separately under its own field.

```bash
curl -i --location 'http://localhost:8080/configs' \
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
        "state_replication_interval": "@every 00h00m10s",
        "register_satellite_interval": "@every 00h00m10s",
        "local_registry": {
            "url": "http://0.0.0.0:8585"
        }
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
Set up the `.env` files. An [example](.env.example) is provided in the repository. Use the token you received earlier in this file.

#### Option 1 : Without dagger
A docker compose file is given in the project root. Tweak it as per your needs. Use the token you received earlier.
> Note : if the ground control is not running on a public IP address, you might need to make sure the satellite and ground control are on the same network so that communication is possible.

#### Option 2 : Using Dagger
```bash
  dagger call build --source=. --component=satellite export --path=./bin
```

#### Option 3 : Using Go
You can directly run : 

```bash
go run cmd/main.go --token "<your token here>" --ground-control-url "<ground control url here>"
```
> Note : by default, logging in JSON format is set to true. To change this pass additional flag `--json-logging=false`



### 7. Configure Local Registry as Mirror (Optional)

Harbor Satellite allows you to set up a local registry as a mirror for upstream registries. Using the optional `--mirrors` flag, you can specify which upstream registries should be mirrored. The configured CRI will attempt to pull images from the local registry (Zot by default) first, and use the upstream registry as a fallback if the image is not available locally.

#### Supported CRIs
- `docker`
- `crio`
- `podman`
- `containerd`

#### Usage
```bash
--mirrors=containerd:docker.io crio:docker.io
```

#### Notes
- Docker: Only supports mirroring images from docker.io. Use `docker:true` to enable Docker mirroring.
- Appending or updating CRI configuration files requires sudo.
- Satellite assumes default configuration paths for each CRI. If you use non-standard locations, you may need to manually update the configs.
- Containerd: Using outdated versions is not recommended, as some configuration options and styles may be deprecated.


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

