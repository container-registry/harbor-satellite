---
title: "Installation"
weight: 3
---

This guide covers all methods for installing Ground Control and Satellite. For a full end-to-end walkthrough with SPIFFE, see the [Quickstart](quickstart.md).

## Prerequisites

- A running Harbor registry with at least one project and image pushed
- Harbor admin credentials (or credentials with robot account creation permissions)
- For Helm installs: a Kubernetes cluster and `helm` CLI installed
- For Docker Compose: Docker and Docker Compose installed

## Installing Ground Control

Ground Control is the cloud-side management service. It needs a PostgreSQL database and access to your Harbor instance.

### Ground Control Binary

Download the latest release for your platform:

```bash
# Linux amd64
curl -Lo ground-control.tar.gz \
  https://github.com/container-registry/harbor-satellite/releases/latest/download/ground-control_Linux_x86_64.tar.gz
tar xzf ground-control.tar.gz
```

Create a `.env` file (see `ground-control/.env.example` for all options):

```bash
cat > .env << 'EOF'
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=https://harbor.example.com
PORT=8080
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres
DB_PASSWORD=password
EOF
```

Run (requires a running PostgreSQL instance):

```bash
./ground-control
```

### Ground Control Docker Compose

The [`ground-control/docker-compose.yml`](https://github.com/container-registry/harbor-satellite/blob/main/ground-control/docker-compose.yml) in the repository runs Ground Control with PostgreSQL:

```bash
cd ground-control
docker compose up -d
```

Override defaults with environment variables:

```bash
HARBOR_URL=https://my-harbor.example.com \
HARBOR_PASSWORD=MyPassword \
docker compose up -d
```

Verify:

```bash
curl http://localhost:8080/health
```

### Helm (Kubernetes)

{{< callout type="warning" >}}
The Helm chart is experimental and not fully tested. Use it at your own risk in production environments.
{{< /callout >}}

Install with the [Helm chart](https://github.com/container-registry/harbor-satellite/tree/main/deploy/helm/ground-control):

```bash
helm install ground-control deploy/helm/ground-control \
  --set harbor.url=https://harbor.example.com \
  --set harbor.username=admin \
  --set harbor.password=Harbor12345 \
  --set database.password=securepassword
```

This deploys Ground Control and an internal PostgreSQL StatefulSet. To use an external database:

```bash
helm install ground-control deploy/helm/ground-control \
  --set harbor.url=https://harbor.example.com \
  --set harbor.username=admin \
  --set harbor.password=Harbor12345 \
  --set database.internal.enabled=false \
  --set database.host=my-postgres.example.com \
  --set database.username=gcuser \
  --set database.password=securepassword
```

To enable SPIFFE:

```bash
helm install ground-control deploy/helm/ground-control \
  --set spiffe.enabled=true \
  --set spiffe.trustDomain=harbor-satellite.local \
  --set harbor.url=https://harbor.example.com \
  --set harbor.password=Harbor12345
```

See `deploy/helm/ground-control/values.yaml` for all configurable values.

## Installing Satellite

Satellite runs at each edge location. It needs a Ground Control URL and either a token or SPIFFE agent.

### Satellite Binary

Download the latest release:

```bash
# Linux amd64
curl -Lo satellite.tar.gz \
  https://github.com/container-registry/harbor-satellite/releases/latest/download/harbor-satellite_Linux_x86_64.tar.gz
tar xzf satellite.tar.gz
```

Run with token-based auth:

```bash
./harbor-satellite \
  --ground-control-url http://gc.example.com:8080 \
  --token "<your-satellite-token>"
```

Run with SPIFFE auth:

```bash
./harbor-satellite \
  --ground-control-url https://gc.example.com:8080 \
  --spiffe-enabled \
  --spiffe-endpoint-socket unix:///run/spire/sockets/agent.sock
```

### Satellite Docker Compose

The root `docker-compose.yml` runs the satellite:

```bash
# Edit docker-compose.yml with your token and Ground Control URL
docker compose up -d
```

Environment variables in the compose file:

- `GROUND_CONTROL_URL` - Ground Control endpoint
- `TOKEN` - Satellite token (token-based auth)

### Satellite Docker Run

```bash
docker run -d \
  --name satellite \
  -e GROUND_CONTROL_URL=http://gc.example.com:8080 \
  -e TOKEN="<your-satellite-token>" \
  -p 8585:8585 \
  registry.goharbor.io/harbor-satellite/satellite:latest
```

For SPIFFE auth, mount the SPIRE agent socket:

```bash
docker run -d \
  --name satellite \
  -e GROUND_CONTROL_URL=https://gc.example.com:8080 \
  -e SPIFFE_ENABLED=true \
  -e SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock \
  -v /run/spire/sockets:/run/spire/sockets:ro \
  -p 8585:8585 \
  registry.goharbor.io/harbor-satellite/satellite:latest
```

## Authentication Flows

### Token-based (Simple)

Best for development and testing. No SPIFFE infrastructure needed.

1. Start Ground Control
2. Register a satellite via the Ground Control API:

   ```bash
   curl -X POST http://localhost:8080/satellites \
     -H "Content-Type: application/json" \
     -d '{"name": "edge-01", "groups": ["my-group"], "config_name": "default"}'
   ```

3. Copy the token from the response
4. Pass it to the satellite binary with `--token`

For a full token-based walkthrough, see [deploy/no-spiffe/quickstart.md](https://github.com/container-registry/harbor-satellite/blob/main/deploy/no-spiffe/quickstart.md).

### SPIFFE/SPIRE (Production)

Uses cryptographic identity instead of static tokens. After a one-time bootstrap with a SPIRE join token, all credentials are handled automatically.

Overview:

1. Deploy a SPIRE server and agent alongside Ground Control
2. Register a satellite via the API (creates SPIRE workload entry + join token)
3. Deploy a SPIRE agent at the edge with the join token
4. Start the satellite with `--spiffe-enabled` (no token needed)
5. Satellite gets its identity from SPIRE and registers with Ground Control over mTLS

Three attestation methods are supported for SPIRE agents:

- **Join Token** - One-time token, simplest to set up
- **X.509 PoP** - Pre-provisioned certificates from your PKI
- **SSH PoP** - SSH host certificates from your SSH CA

For a full SPIFFE walkthrough, see the [Quickstart](quickstart.md).

## Post-install

### Creating Groups and Assigning Images

Groups are collections of images that satellites replicate. Create a group and add images:

```bash
curl -X POST http://localhost:8080/groups/sync \
  -H "Content-Type: application/json" \
  -d '{
    "group": "edge-images",
    "registry": "https://harbor.example.com",
    "artifacts": [
      {
        "repository": "library/nginx",
        "tag": ["alpine"],
        "type": "image",
        "digest": "sha256:..."
      }
    ]
  }'
```

Assign the group to a satellite:

```bash
curl -X POST http://localhost:8080/groups/satellite \
  -H "Content-Type: application/json" \
  -d '{"satellite": "edge-01", "group": "edge-images"}'
```

### Verifying Replication

After assigning a group, the satellite begins replicating images on its next sync interval (default: 10 seconds). Check the satellite logs for replication activity and verify images are available locally:

```bash
crane catalog localhost:8585
```

Or pull directly:

```bash
docker pull localhost:8585/library/nginx:alpine
```

### Configuring CRI Mirroring

Configure container runtimes to use the satellite's local registry as a mirror:

```bash
# containerd: mirror docker.io and quay.io
./harbor-satellite --mirrors=containerd:docker.io,quay.io ...

# Docker: mirror docker.io (only registry Docker supports mirroring)
./harbor-satellite --mirrors=docker:true ...

# Podman
./harbor-satellite --mirrors=podman:docker.io ...

# CRI-O
./harbor-satellite --mirrors=crio:docker.io,quay.io ...
```

Notes:

- CRI config changes require `sudo` (satellite modifies system config files)
- Docker requires a service restart after config changes
- Multiple `--mirrors` flags can be combined
