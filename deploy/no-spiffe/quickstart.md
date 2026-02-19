# Harbor Satellite Quick Start Guide (Token-based ZTR)

This guide walks through setting up Harbor Satellite using token-based Zero-Touch Registration. This approach is best suited for development, testing, and simple deployments.

For production deployments with cryptographic identity and mTLS, see the [SPIFFE/SPIRE quickstart](../quickstart/README.md).

## Prerequisites

- A Harbor registry instance with the satellite adapter installed. You can find the instance [here](https://github.com/container-registry/harbor-next/tree/satellite).
- Credentials with permission to create robot accounts in the registry
- The latest version of Dagger installed. [Download and install Dagger](https://docs.dagger.io/install).
- (Optional) Docker and Docker Compose for non-Dagger setups. [Install Docker](https://docs.docker.com/get-docker/).

## Step 1: Configure Ground Control

Ground Control is the central service that manages satellite configurations.

1. Clone the Harbor Satellite repository (if not already done):
   ```bash
   git clone https://github.com/container-registry/harbor-satellite.git
   cd harbor-satellite
   ```
2. Navigate to the `ground-control` directory:
   ```bash
   cd ground-control
   ```
3. Create a `.env` file using the provided example:
   ```bash
   cp .env.example .env
   ```
4. Edit the `.env` file with your configuration:

   ```env
   # Harbor Registry Credentials
   HARBOR_USERNAME=admin
   HARBOR_PASSWORD=Harbor12345
   HARBOR_URL=https://demo.goharbor.io

   # Ground Control Settings
   PORT=8080
   APP_ENV=local

   # Database Settings (Use DB_HOST=pgservice for Dagger)
   DB_HOST=127.0.0.1
   DB_PORT=5432
   DB_DATABASE=groundcontrol
   DB_USERNAME=postgres
   DB_PASSWORD=password
   ```

   > Note: Ensure the database is running and accessible. For Dagger, set `DB_HOST=pgservice`.

## Step 2: Start Ground Control

Choose one of the following options to start Ground Control.

### Option 1: Using Docker Compose (Recommended for End Users)

1. Update the `docker-compose.yml` file in the `ground-control` directory with the same credentials as in the `.env` file.

2. Start Ground Control:

   ```bash
   docker compose up
   ```

   > Tip: Use `-d` to run in detached mode. Verify the service is running with `docker ps`.

### Option 2: Build and Run Binary

1. Build the Ground Control binary:

   ```bash
   dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev
   ```

2. Run the binary:

   ```bash
   ./gc-dev
   ```

### Option 3: Using Dagger (Recommended for Developers)

1. Start Ground Control with Dagger:

   ```bash
   dagger call run-ground-control up
   ```

## Step 3: Verify Ground Control Health

Check if Ground Control is running:

```bash
curl http://localhost:8080/health
```

A `200 OK` response indicates Ground Control is healthy.

## Step 4: Create a Group for Artifacts

A group is a set of images that the satellite needs to replicate from the upstream registry. It also contains information about all the artifacts present in it.

> Note: Modify the body below according to your registry.

```bash
curl -X POST http://localhost:8080/api/groups/sync \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "group": "group1",
    "registry": "https://demo.goharbor.io",
    "artifacts": [
      {
        "repository": "satellite/alpine",
        "tag": ["latest"],
        "type": "docker",
        "digest": "sha256:5a6ee6c36824d527a0fe91a2a7c160c2e286bbeae46cd931c337ac769f1bd930",
        "deleted": false
      }
    ]
  }'
```

> Note: Replace `repository`, `tag`, and `digest` with your artifact details. Use `docker inspect` or Harbor's UI to find the digest.

## Step 5: Configure the Satellite

Create a config artifact for the satellite. See [config example](https://github.com/container-registry/harbor-satellite/blob/main/examples/config.json). This artifact tells the satellite where Ground Control is located and defines how and when to replicate artifacts. It also includes the local OCI-compliant registry configuration.

```bash
curl -i --location 'http://localhost:8080/api/configs' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer ${AUTH_TOKEN}" \
--data '{
  "config_name": "config1",
  "registry": "http://demo.goharbor.io",
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

> Tip: Adjust `ground_control_url` and `local_registry.url` if running on a different host or port.

## Step 6: Register the Satellite

Register the satellite with the group and configuration created earlier. This request returns a token. Save it for the next step.

```bash
curl --location 'http://localhost:8080/api/satellites' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer ${AUTH_TOKEN}" \
--data '{
    "name": "satellite_1",
    "groups": ["group1"],
    "config_name": "config1"
}'
```

> Important: Copy the token from the response and store it securely.

## Step 7: Start the Satellite

Use the token from Step 6 to start the satellite. See [.env.example](https://github.com/container-registry/harbor-satellite/blob/main/.env.example).

### Option 1: Using Docker Compose (Recommended for End Users)

1. Update the `docker-compose.yml` file in the project root with the token and other required settings.
2. Ensure Ground Control and the satellite are on the same network if Ground Control isn't on a public IP.
3. Start the satellite:

   ```bash
   docker compose up -d
   ```

### Option 2: Using Dagger

1. Build and export the satellite binary:

   ```bash
   dagger call build --source=. --component=satellite export --path=./bin
   ```

2. Run the binary with the token:

   ```bash
   ./bin --token "<your-token>" --ground-control-url "http://127.0.0.1:8080"
   ```

### Option 3: Using Go

1. Run the satellite directly:

   ```bash
   go run cmd/main.go --token "<your token here>" --ground-control-url "<ground control url here>"
   ```

   > Note: By default, JSON logging is enabled. To disable it, pass `--json-logging=false`.

## Configure Local Registry as Mirror (Optional)

Harbor Satellite allows setting up a local registry as a mirror for upstream registries. Using the `--mirrors` flag, you can specify which upstream registries to mirror. The configured container runtime will pull from the local registry (Zot by default) first, falling back to the upstream registry.

### Supported CRIs
- `docker`
- `crio`
- `podman`
- `containerd`

### Usage
```bash
--mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io
```

### Notes
- Docker only supports mirroring images from docker.io. Use `--mirrors=docker:true` to enable Docker mirroring.
- For loading dockerd's configs, the docker service is restarted. Make sure you have stopped all other docker processes.
- Appending or updating CRI configuration files requires sudo.
- Satellite assumes default configuration paths for each CRI. If you use non-standard locations, you may need to manually update the configs.
- Containerd: Using outdated versions is not recommended, as some configuration options may be deprecated.

## Troubleshooting

1. Ground Control Connection Issues
   - Verify the `ground_control_url` in the satellite configuration.
   - Check if Ground Control is running: `curl http://localhost:8080/health`.
   - Ensure environment variables match the `docker-compose.yml` file.

2. Registry Access Issues
   - Confirm Harbor credentials (`HARBOR_USERNAME` and `HARBOR_PASSWORD`).
   - Test network connectivity to the Harbor registry: `ping demo.goharbor.io`.
   - Ensure the robot account has appropriate permissions in Harbor.

3. Satellite Not Replicating Artifacts
   - Verify the group and config names in the satellite registration.
   - Check the artifact digest and repository details in the group configuration.
   - Ensure the local registry (`http://127.0.0.1:8585`) is running.

## Need Help?

- Explore the [Harbor Satellite documentation](https://docs.goharbor.io).
- Join the [Harbor community](https://community.goharbor.io) for support.
- Open an issue on GitHub: https://github.com/container-registry/harbor-satellite/issues
