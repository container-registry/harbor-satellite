# Harbor Satellite Quick Start Guide (Token-based ZTR)

This guide walks through setting up Harbor Satellite using token-based Zero-Touch Registration. This approach is best suited for development, testing, and simple deployments.

For production deployments with cryptographic identity and mTLS, see the [SPIFFE/SPIRE quickstart](../spiffe/README.md).

## Prerequisites

- A Harbor instance and a Harbor account that can create projects and robot accounts. Groups are created through the Ground Control API, so no Harbor-side changes are needed.
- [Go](https://go.dev/dl/) 1.26.5 and [Task](https://taskfile.dev/installation/), to build the binaries (Options 2 and 3 below).
- [Docker](https://docs.docker.com/get-docker/) and Docker Compose, for PostgreSQL and the compose options.

## Step 1: Configure Ground Control

Ground Control is the central service that manages satellite configurations.

1. Clone the Harbor Satellite repository (if not already done):
   ```bash
   git clone https://github.com/container-registry/harbor-satellite.git
   cd harbor-satellite
   ```
2. Create an environment file from the root example. The Ground Control and satellite binaries read `.env` from the current directory:
   ```bash
   cp .env.example .env
   ```
3. Edit the `.env` file with your configuration:

   ```env
   # Harbor Registry Credentials
   HARBOR_USERNAME=admin
   HARBOR_PASSWORD=Harbor12345
   HARBOR_URL=https://demo.goharbor.io

   # Password for the Ground Control "admin" user, created on first startup (required)
   ADMIN_PASSWORD=ChangeMe123

   # Ground Control Settings
   PORT=8080

   # Database Settings (PostgreSQL from docker-compose.yml, published on host port 8100)
   DB_HOST=127.0.0.1
   DB_PORT=8100
   DB_DATABASE=groundcontrol
   DB_USERNAME=postgres
   DB_PASSWORD=password
   ```

   > Note: `ADMIN_PASSWORD` must satisfy the password policy (default: at least 8 characters with an uppercase letter, a lowercase letter and a number). Ground Control exits on startup if it is missing or invalid.

## Step 2: Start Ground Control

Choose one of the following options to start Ground Control.

### Option 1: Using Docker Compose

The root `docker-compose.yml` takes `HARBOR_URL` (required), `HARBOR_USERNAME`, `HARBOR_PASSWORD` and `ADMIN_PASSWORD` from `.env` or the shell, and sets the database settings itself. It also reads `SKIP_HARBOR_HEALTH_CHECK`, which defaults to `true` when unset; `.env.example` sets it to `false`, so Ground Control fails fast when Harbor is unreachable.

```bash
docker compose up -d
```

This starts PostgreSQL and Ground Control. The satellite service is skipped until you start it with a token in Step 8.

> Tip: Verify the services are running with `docker compose ps`. Ground Control listens on `localhost:8080` (`GC_HOST_PORT`).

### Option 2: Build and Run Binary

1. Start PostgreSQL from the compose file (published on host port `8100`, matching `DB_PORT` above):

   ```bash
   docker compose up -d postgres
   ```

2. Build the Ground Control binary (written to `bin/ground-control`):

   ```bash
   task _build:ground-control
   ```

3. Run the binary from the repository root so it picks up `.env`:

   ```bash
   ./bin/ground-control
   ```

## Step 3: Verify Ground Control Health

Check if Ground Control is running:

```bash
curl http://localhost:8080/health
```

A `200 OK` response with `{"status":"healthy"}` indicates Ground Control and its database are up.

## Step 4: Log In

All management endpoints are under `/api` and require an `Authorization` header. Log in as `admin` with `ADMIN_PASSWORD` to get a session token:

```bash
export ADMIN_PASSWORD=ChangeMe123
LOGIN_RESP=$(curl -s -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD}\"}")
AUTH_TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
```

The response is `{"token":"...","expires_at":"..."}`. The session lasts 24 hours by default (`SESSION_DURATION`).

## Step 5: Create a Group for Artifacts

A group is a set of images that the satellite needs to replicate from the upstream registry. It also contains information about all the artifacts present in it.

> Note: Modify the body below according to your registry. Ground Control always uses `HARBOR_URL` as the source registry, so the request has no `registry` field.

```bash
curl -X POST http://localhost:8080/api/groups/sync \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "group": "group1",
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

> Note: Replace `repository`, `tag`, and `digest` with your artifact details. `digest` is optional: with it the satellite replicates exactly that digest, without it the satellite follows the tag. Use `docker inspect` or Harbor's UI to find the digest.

## Step 6: Create a Satellite Config

Create a config for the satellite. For all available fields, see the [satellite config example](../../config.json). This config tells the satellite where Ground Control is located and defines how and when to replicate artifacts. By default, replicated content is stored in an OCI image layout at `<config-dir>/oci`.

```bash
curl -i -X POST http://localhost:8080/api/configs \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
  "config_name": "config1",
  "config": {
    "state_config": {},
    "app_config": {
      "ground_control_url": "http://127.0.0.1:8080",
      "log_level": "info",
      "use_unsecure": true,
      "state_replication_interval": "@every 00h00m10s",
      "register_satellite_interval": "@every 00h00m10s",
      "bring_own_registry": false
    }
  }
}'
```

> Tip: The satellite's `--ground-control-url` / `GROUND_CONTROL_URL` takes precedence over `ground_control_url` in this config. `use_unsecure: true` makes the satellite talk plain HTTP to Harbor; set it to `false` for an HTTPS Harbor. A non-empty `USE_UNSECURE` environment variable on the satellite overrides this value.

## Step 7: Register the Satellite

Register the satellite with the group and configuration created earlier. Both must exist, otherwise the request fails. The response is `{"token":"..."}`. The token is single-use and expires after 24 hours.

```bash
curl -X POST http://localhost:8080/api/satellites \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "name": "satellite_1",
    "groups": ["group1"],
    "config_name": "config1"
  }'
```

> Important: Copy the token from the response and store it securely.

This endpoint is disabled when Ground Control runs with SPIFFE enabled. Use the [SPIFFE/SPIRE quickstart](../spiffe/README.md) in that case.

## Step 8: Start the Satellite

Use the token from Step 7 to start the satellite. The satellite also needs the Harbor registry address as reachable from the satellite host (`--harbor-registry-url` or `HARBOR_REGISTRY_URL`). It replaces the scheme and host of the Harbor URL that Ground Control hands out, and the satellite exits without it. See [.env.example](../../../.env.example) for all satellite environment variables.

### Option 1: Using Docker Compose

The `satellite` service in the root `docker-compose.yml` connects to the `ground-control` service. `HARBOR_REGISTRY_URL` defaults to `HARBOR_URL`; set it in `.env` if Harbor is reachable under a different address from the container. For a plain-HTTP Harbor, also set `USE_UNSECURE=true`.

```bash
TOKEN=<your-token> docker compose up -d satellite
```

### Option 2: Build and Run Binary

1. Build the satellite binary (written to `bin/satellite`):

   ```bash
   task _build:satellite
   ```

2. Run the binary with the token:

   ```bash
   ./bin/satellite --token "<your-token>" --ground-control-url "http://127.0.0.1:8080" \
     --harbor-registry-url "https://demo.goharbor.io"
   ```

### Option 3: Using Go

1. Run the satellite directly:

   ```bash
   go run ./cmd/satellite --token "<your token here>" --ground-control-url "<ground control url here>" \
     --harbor-registry-url "<harbor registry url here>"
   ```

   > Note: By default, JSON logging is enabled. To disable it, pass `--json-logging=false`.

## Step 9: Verify Replication

The satellite writes replicated images to an OCI image layout in `<config-dir>/oci`. It does not serve them over HTTP, so check the layout on disk. The config directory is `CONFIG_DIR` (`/data` in the compose file) or `--config-dir`; the default is `satellite` under the user config directory (`~/.config/satellite` on Linux).

```bash
# Docker Compose
docker exec satellite test -f /data/oci/oci-layout
docker exec satellite sh -c \
  'grep -o "org.opencontainers.image.ref.name[^}]*" /data/oci/index.json'

# Binary on Linux
grep -o "org.opencontainers.image.ref.name[^}]*" ~/.config/satellite/oci/index.json
```

Ground Control also lists what the satellite reported in its last heartbeat:

```bash
curl -s http://localhost:8080/api/satellites/satellite_1/images \
  -H "Authorization: Bearer ${AUTH_TOKEN}"
```

## Cleanup

With Docker Compose, stop everything and remove the volumes. The profile flag includes the satellite service:

```bash
docker compose --profile satellite down -v
```

## Configure a BYO Registry as Mirror (Optional)

Harbor Satellite can configure an external BYO registry as a mirror for upstream registries. Start Satellite with `--byo-registry --registry-url <url>` and use `--mirrors` to select the upstream registries. The default local OCI layout does not expose a registry endpoint and cannot be used as a CRI mirror.

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
   - With Docker Compose, check that `.env` sets `HARBOR_URL`, `HARBOR_USERNAME`, `HARBOR_PASSWORD` and `ADMIN_PASSWORD`: `docker compose logs ground-control`.

2. Registry Access Issues
   - Confirm Harbor credentials (`HARBOR_USERNAME` and `HARBOR_PASSWORD`).
   - Test network connectivity to the Harbor registry: `curl -I https://demo.goharbor.io/v2/`.
   - Ensure the Harbor account can create projects and robot accounts.

3. Satellite Not Replicating Artifacts
   - Verify the group and config names in the satellite registration.
   - Verify `--harbor-registry-url` / `HARBOR_REGISTRY_URL` points to the Harbor instance set in `HARBOR_URL`.
   - Check the artifact digest and repository details in the group configuration.
   - Check Satellite logs and verify that `<config-dir>/oci/oci-layout` exists.

## Need Help?

- Explore the [Harbor Satellite documentation](https://docs.goharbor.io).
- Join the [Harbor community](https://community.goharbor.io) for support.
- Open an issue on GitHub: https://github.com/container-registry/harbor-satellite/issues
