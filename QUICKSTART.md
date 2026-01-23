# Harbor Satellite Quick Start Guide

Welcome to the Harbor Satellite Quick Start Guide! This guide provides a clear and streamlined process to set up and start using Harbor Satellite quickly.

## Prerequisites

Before you begin, ensure you have:

- A **Harbor registry instance** with the satellite adapter installed. You can find it in the [harbor-next satellite branch](https://github.com/container-registry/harbor-next/tree/satellite).
- **Credentials** with permission to create robot accounts in the registry

- The latest version of **Dagger** installed. [Download and install Dagger](https://docs.dagger.io/install).
- (Optional) **Docker** and **Docker Compose** for non-Dagger setups. [Install Docker](https://docs.docker.com/get-docker/).

## Step 1: Configure Ground Control
```bash
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=http://localhost:8080

PORT=9090
ADMIN_PASSWORD=SecurePass123

# Password Policy (optional)
PASSWORD_MIN_LENGTH=8
PASSWORD_MAX_LENGTH=128
PASSWORD_REQUIRE_UPPERCASE=true
PASSWORD_REQUIRE_LOWERCASE=true
PASSWORD_REQUIRE_NUMBER=true
PASSWORD_REQUIRE_SPECIAL=false

DB_HOST=127.0.0.1 # For Dagger use DB_HOST=pgservice
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres
DB_PASSWORD=password
```

> **Note:** By default, passwords must be at least 8 characters and contain uppercase, lowercase, and a number.
You can also directly edit this [example](ground-control/.env.example) available in the repository.

Ground Control is the central service that manages satellite configurations. Let’s set it up.

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
   HARBOR_URL=http://localhost:8080

   # Ground Control Settings
   PORT=9090
   ADMIN_PASSWORD=SecurePass123

   # Password Policy (optional)
   PASSWORD_MIN_LENGTH=8
   PASSWORD_REQUIRE_UPPERCASE=true
   PASSWORD_REQUIRE_LOWERCASE=true
   PASSWORD_REQUIRE_NUMBER=true
   ```

   > **Note:** _Database settings are configured in docker-compose.yml. For Dagger, set `DB_HOST=pgservice`._
   > **Note:** _ADMIN_PASSWORD must meet the password policy requirements (default: 8+ chars with uppercase, lowercase, and number)._

## Step 2: Start Ground Control

Choose one of the following options to start Ground Control

**Option 1: Using Docker Compose (Recommended for End Users)**

1. Start Ground Control:

   ```bash
   docker compose up
   ```

   > **Tip:** _Use `-d` to run in detached mode. Verify the service is running with `docker ps`._

**Option 2: Build and Run Binary (Alternative for End Users)**

1. Build the Ground Control binary:

   ```bash
   dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev
   ```

2. Run the binary:

   ```bash
   ./gc-dev
   ```

**Option 3: Using Dagger (Recommended for Developers)**

1. Start Ground Control with Dagger:

   ```bash
   dagger call run-ground-control up
   ```

## Step 3: Verify Ground Control Health

Check if Ground Control is running

   ```bash
   curl http://localhost:9090/health
   ```

A `200 OK` response indicates Ground Control is healthy.

## Step 4: Create a Group for Artifacts

A **group** is just a set of images that the satellite needs to replicate from the upstream registry.It also consists information about all the artifacts present in it.

> **Note:** _You must modify the body given below according to your registry._


Use the following `curl` command to create a group. Modify the JSON body to match your registry and artifacts:

```bash
curl -X POST http://localhost:9090/groups/sync \
  -H "Content-Type: application/json" \
  -d '{
    "group": "group1",
    "registry": "http://localhost:8080",
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


> **Note:** _Replace `repository`, `tag`, and `digest` with your artifact details. Use `docker inspect` or Harbor's UI to find the digest._

## Step 5: Configure the Satellite

Now you need to create a config artifact for the satellite. An example is given [example](https://github.com/container-registry/harbor-satellite/blob/main/examples/config.json). This artifact tells the satellite where the ground control is located and defines how and when to replicate artifacts from it. It also includes details about the local OCI-compliant registry, specified separately under its own field.

```bash
curl -i --location 'http://localhost:9090/configs' \
--header 'Content-Type: application/json' \
--data '{
  "config_name": "config1",
  "config": {
    "state_config": {},
    "app_config": {
      "ground_control_url": "http://host.docker.internal:9090",
      "log_level": "info",
      "use_unsecure": true,
      "state_replication_interval": "@every 00h00m10s",
      "register_satellite_interval": "@every 00h00m10s",
      "local_registry": {
        "url": "http://0.0.0.0:8585"
      },
      "heartbeat_interval": "@every 00h00m30s"
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
}'
```

> **Tip:** _Adjust `ground_control_url` and `local_registry.url` if running on a different host or port_.

## Step 6: Register the Satellite

Register the satellite with the group and configuration created earlier. This request returns a token, which you must save for the next step:

```bash
curl --location 'http://localhost:9090/satellites' \
--header 'Content-Type: application/json' \
--data '{
    "name": "satellite_1",
    "groups": ["group1"],
    "config_name": "config1"
}'
```

> **Important**: Copy the token from the response. _Where will you store this token to ensure it’s secure and accessible?_

## Step 7: Start the Satellite

Set up the satellite using the token from Step 6. Choose one of the following options: Example [here](https://github.com/container-registry/harbor-satellite/blob/main/.env.example)

**Option 1: Using Docker Compose (Recommended for End Users)**

1. Start the satellite with your token:

   ```bash
   TOKEN=<your-token> docker compose up -d
   ```

   > **Note:** _Ground Control URL is pre-configured in `.env` to use `host.docker.internal:9090`._

**Option 2: Using Dagger**

1. Build and export the satellite binary:

   ```bash
   dagger call build --source=. --component=satellite export --path=./bin
   ```

2. Run the binary with the token:

   ```bash
   ./bin --token "<your-token>" --ground-control-url "http://host.docker.internal:9090"
   ```

**Option 3: Using Go**

1. Run the satellite directly:
   
   ```bash
    go run cmd/main.go --token "<your token here>" --ground-control-url "<ground control url here>"
   ```


   > **Note** : by default, logging in JSON format is set to true.  To change this pass additional flag `--json-logging=false` 



### 7. Configure Local Registry as Mirror (Optional)

Harbor Satellite allows you to set up a local registry as a mirror for upstream registries. Using the optional `--mirrors` flag, you can specify which upstream registries should be mirrored. The configured container runtime interface (CRI) will attempt to pull images from the local registry (Zot by default) first, and use the upstream registry as a fallback if the image is not available locally.
#### Supported CRIs
- `docker`
- `crio`
- `podman`
- `containerd`

#### Usage
```bash
--mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io
```

#### Notes
- When using docker as a runtime it supports mirroring images from docker.io. So, use `--mirrors=docker:true` to enable Docker mirroring. 
- For loading dockerd's configs docker service is restarted. Make sure you have stopped all other docker processes
- Appending or updating CRI configuration files requires sudo.
- Satellite assumes default configuration paths for each CRI. If you use non-standard locations, you may need to manually update the configs.
- Containerd: Using outdated versions is not recommended, as some configuration options and styles may be deprecated.


## Troubleshooting

If you encounter issues, consider these common problems and solutions:

1. **Ground Control Connection Issues**
   - Verify the `ground_control_url` in the satellite configuration.
   - Check if Ground Control is running: `curl http://localhost:9090/health`.
   - Ensure environment variables in `.env` file are correct.
2. **Registry Access Issues**
   - Confirm Harbor credentials (`HARBOR_USERNAME` and `HARBOR_PASSWORD`).
   - Test network connectivity to the Harbor registry: `curl http://localhost:8080/api/v2.0/health`.
   - Ensure the robot account has appropriate permissions in Harbor.
3. **Satellite Not Replicating Artifacts**
   - Verify the group and config names in the satellite registration.
   - Check the artifact digest and repository details in the group configuration..
   - Ensure the local registry (`http://127.0.0.1:8585`) is running.

> **Question**: Which of these issues seems most likely in your environment, and how will you verify it?
## Need Help?

- Explore the [Harbor Satellite documentation](https://docs.goharbor.io).
- Join the [Harbor community](https://community.goharbor.io) for support.
- Open an issue on GitHub: https://github.com/container-registry/harbor-satellite/issues

