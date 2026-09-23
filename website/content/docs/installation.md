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

Ground Control is the cloud-side management service. It needs a PostgreSQL database and access to your Harbor instance. On startup it checks that Harbor is reachable (skip with `SKIP_HARBOR_HEALTH_CHECK=true`, testing only), runs database migrations, and creates the `admin` user from `ADMIN_PASSWORD` if no system admin exists yet.

`ADMIN_PASSWORD` is required on first start. Ground Control exits if it is empty or violates the password policy (default: 8 to 128 characters with an uppercase letter, a lowercase letter and a number).

### Ground Control Binary

Release archives are versioned (see the [releases page](https://github.com/container-registry/harbor-satellite/releases)). The binary reads SQL migrations from `/migrations` or `./internal/groundcontrol/sql/schema`, and the archive does not include them, so run it from a checkout of the matching tag:

```bash
VERSION=0.0.6
git clone --depth 1 --branch v${VERSION} https://github.com/container-registry/harbor-satellite.git
cd harbor-satellite

# Linux amd64. Other platforms: ground-control_${VERSION}_<os>_<arch>.tar.gz
curl -Lo ground-control.tar.gz \
  https://github.com/container-registry/harbor-satellite/releases/download/v${VERSION}/ground-control_${VERSION}_linux_amd64.tar.gz
tar xzf ground-control.tar.gz ground-control
```

Create a `.env` file in the same directory (see [`.env.example`](https://github.com/container-registry/harbor-satellite/blob/main/.env.example) for all options):

```bash
cat > .env << 'EOF'
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=https://harbor.example.com
ADMIN_PASSWORD=ChangeMe123
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

The [`docker-compose.yml`](https://github.com/container-registry/harbor-satellite/blob/main/docker-compose.yml) in the repository builds Ground Control from source and runs it with PostgreSQL. `HARBOR_URL` and `ADMIN_PASSWORD` have no usable default and must be set:

```bash
git clone https://github.com/container-registry/harbor-satellite.git
cd harbor-satellite
HARBOR_URL=https://harbor.example.com \
HARBOR_USERNAME=admin \
HARBOR_PASSWORD=Harbor12345 \
ADMIN_PASSWORD=ChangeMe123 \
docker compose up -d postgres ground-control
```

Compose also reads these variables from a `.env` file in the repository root. The compose file defaults `SKIP_HARBOR_HEALTH_CHECK` to `true` (testing only); a `.env` copied from `.env.example` sets it to `false`, so Harbor must then be reachable at startup. Ground Control listens on host port 8080 (`GC_HOST_PORT` to change it), PostgreSQL on host port 8100.

Verify:

```bash
curl http://localhost:8080/health
# {"status":"healthy"}
```

### Helm (Kubernetes)

{{< callout type="warning" >}}
The Helm chart is experimental and not fully tested. Use it at your own risk in production environments.
{{< /callout >}}

Install with the [Helm chart](https://github.com/container-registry/harbor-satellite/tree/main/examples/deploy/helm/ground-control):

```bash
helm install ground-control examples/deploy/helm/ground-control \
  --set harbor.url=https://harbor.example.com \
  --set harbor.username=admin \
  --set harbor.password=Harbor12345 \
  --set adminPassword=ChangeMe123 \
  --set database.password=securepassword
```

`harbor.password`, `adminPassword` and `database.password` are required. The chart fails to render without them.

This deploys Ground Control and an internal PostgreSQL StatefulSet. To use an external database:

```bash
helm install ground-control examples/deploy/helm/ground-control \
  --set harbor.url=https://harbor.example.com \
  --set harbor.username=admin \
  --set harbor.password=Harbor12345 \
  --set adminPassword=ChangeMe123 \
  --set database.internal.enabled=false \
  --set database.host=my-postgres.example.com \
  --set database.username=gcuser \
  --set database.password=securepassword
```

To enable SPIFFE:

```bash
helm install ground-control examples/deploy/helm/ground-control \
  --set spiffe.enabled=true \
  --set spiffe.trustDomain=harbor-satellite.local \
  --set harbor.url=https://harbor.example.com \
  --set harbor.password=Harbor12345 \
  --set adminPassword=ChangeMe123 \
  --set database.password=securepassword
```

With `spiffe.enabled=true` Ground Control serves HTTPS with SPIFFE mTLS and expects a SPIRE agent Workload API socket at `spiffe.endpointSocket`. The chart does not deploy SPIRE.

See `examples/deploy/helm/ground-control/values.yaml` for all configurable values.

## Installing Satellite

Satellite runs at each edge location. Required settings:

| Flag | Env var | Required |
|------|---------|----------|
| `--ground-control-url` | `GROUND_CONTROL_URL` | Always |
| `--harbor-registry-url` | `HARBOR_REGISTRY_URL` | Always. The Harbor URL as reachable from the edge device, it overrides the URL Ground Control returns |
| `--token` | `TOKEN` | Unless `--spiffe-enabled` / `SPIFFE_ENABLED=true` |
| `--registry-url` | `REGISTRY_URL` | With `--byo-registry` / `BYO_REGISTRY=true` |

Other options: `--config-dir` / `CONFIG_DIR` (default `~/.config/satellite`), `--registry-data-dir` / `REGISTRY_DATA_DIR` (default `<config-dir>/oci`), `--use-unsecure` / `USE_UNSECURE` (plain HTTP to registries), `--spiffe-endpoint-socket`, `--spiffe-expected-server-id`, `--mirrors`, `--direct-delivery` / `DIRECT_DELIVERY`, `--image-dir` / `IMAGE_DIR`, `--shutdown-timeout` / `SHUTDOWN_TIMEOUT` (default 30s). Run `harbor-satellite -h` for the full list.

### Satellite Binary

Release archives are versioned. Each contains a single `harbor-satellite` binary:

```bash
VERSION=0.0.6

# Linux amd64
curl -Lo satellite.tar.gz \
  https://github.com/container-registry/harbor-satellite/releases/download/v${VERSION}/harbor-satellite_${VERSION}_linux_amd64.tar.gz
tar xzf satellite.tar.gz harbor-satellite

# Linux arm64
curl -Lo satellite.tar.gz \
  https://github.com/container-registry/harbor-satellite/releases/download/v${VERSION}/harbor-satellite_${VERSION}_linux_arm64.tar.gz
tar xzf satellite.tar.gz harbor-satellite
```

See the [releases page](https://github.com/container-registry/harbor-satellite/releases) for all available platforms and formats (tar.gz, deb, rpm, apk, archlinux).

### Building from source

```bash
git clone https://github.com/container-registry/harbor-satellite.git
cd harbor-satellite
go build -o harbor-satellite ./cmd/satellite
```

Run with token-based auth:

```bash
./harbor-satellite \
  --ground-control-url http://gc.example.com:8080 \
  --harbor-registry-url https://harbor.example.com \
  --token "<your-satellite-token>"
```

Run with SPIFFE auth:

```bash
./harbor-satellite \
  --ground-control-url https://gc.example.com:8080 \
  --harbor-registry-url https://harbor.example.com \
  --spiffe-enabled \
  --spiffe-endpoint-socket unix:///run/spire/sockets/agent.sock
```

### Satellite Docker Compose

The root `docker-compose.yml` defines a `satellite` service under the `satellite` profile, so it only starts when named explicitly. It connects to the Ground Control service from the same file:

```bash
TOKEN="<your-satellite-token>" \
HARBOR_URL=https://harbor.example.com \
docker compose up -d satellite
```

Environment variables read by the compose file:

- `TOKEN` - Satellite registration token (token-based auth)
- `HARBOR_REGISTRY_URL` - Harbor URL as reachable from the satellite container (defaults to `HARBOR_URL`)
- `USE_UNSECURE` - set to `true` for a plain-HTTP Harbor

### Satellite Docker Run

The satellite container does not listen on any port. Mount a volume for its config and OCI layout:

```bash
docker run -d \
  --name satellite \
  -e GROUND_CONTROL_URL=http://gc.example.com:8080 \
  -e HARBOR_REGISTRY_URL=https://harbor.example.com \
  -e TOKEN="<your-satellite-token>" \
  -e CONFIG_DIR=/data \
  -v satellite-data:/data \
  registry.goharbor.io/harbor-satellite/satellite:latest
```

For SPIFFE auth, mount the SPIRE agent socket:

```bash
docker run -d \
  --name satellite \
  -e GROUND_CONTROL_URL=https://gc.example.com:8080 \
  -e HARBOR_REGISTRY_URL=https://harbor.example.com \
  -e SPIFFE_ENABLED=true \
  -e SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock \
  -e CONFIG_DIR=/data \
  -v satellite-data:/data \
  -v /run/spire/sockets:/run/spire/sockets:ro \
  registry.goharbor.io/harbor-satellite/satellite:latest
```

## Authentication Flows

### Token-based (Simple)

Best for development and testing. No SPIFFE infrastructure needed. This endpoint is disabled when Ground Control runs with SPIFFE enabled.

All `/api/*` endpoints need a session token from `POST /login` (or HTTP Basic auth with a Ground Control user). The satellite must be registered after its config and groups exist:

1. Log in as the `admin` user created from `ADMIN_PASSWORD`:

   ```bash
   GC=http://localhost:8080
   AUTH_TOKEN=$(curl -s -X POST $GC/login \
     -H "Content-Type: application/json" \
     -d '{"username":"admin","password":"ChangeMe123"}' | jq -r .token)
   ```

2. Create a config (the `config` body is optional, defaults apply to omitted fields):

   ```bash
   curl -X POST $GC/api/configs \
     -H "Content-Type: application/json" \
     -H "Authorization: Bearer $AUTH_TOKEN" \
     -d '{"config_name": "default", "config": {"state_config": {}, "app_config": {"log_level": "info"}}}'
   ```

3. Create a group with its images (see [Creating Groups](#creating-groups-and-assigning-images))

4. Register the satellite with the group and config:

   ```bash
   curl -X POST $GC/api/satellites \
     -H "Content-Type: application/json" \
     -H "Authorization: Bearer $AUTH_TOKEN" \
     -d '{"name": "edge-01", "groups": ["edge-images"], "config_name": "default"}'
   ```

5. Copy the `token` from the response. It is single-use and expires after 24 hours.
6. Pass it to the satellite with `--token` (or `TOKEN`). The satellite exchanges it at `POST /satellites/ztr` for Harbor robot credentials.

For a full token-based walkthrough, see [examples/deploy/no-spiffe/quickstart.md](https://github.com/container-registry/harbor-satellite/blob/main/examples/deploy/no-spiffe/quickstart.md).

### SPIFFE/SPIRE (Production)

Uses cryptographic identity instead of static tokens. After a one-time bootstrap with a SPIRE join token, all credentials are handled automatically.

Overview:

1. Deploy a SPIRE server and agent alongside Ground Control
2. Register a satellite with `POST /api/satellites/register` (system admin only). Ground Control creates the SPIRE workload entry, the Harbor robot account and a `default` config, and for the join-token method returns a join token (default TTL 600 seconds, `ttl_seconds` up to 86400)
3. Deploy a SPIRE agent at the edge with the join token, X.509 certificate or SSH host certificate
4. Start the satellite with `--spiffe-enabled` (no `--token` needed)
5. Satellite gets its identity from SPIRE and registers with Ground Control over mTLS at `GET /satellites/spiffe-ztr`

Three attestation methods are supported for SPIRE agents:

- **Join Token** - One-time token, simplest to set up
- **X.509 PoP** - Pre-provisioned certificates from your PKI
- **SSH PoP** - SSH host certificates from your SSH CA

For a full SPIFFE walkthrough, see the [Quickstart](quickstart.md).

## Post-install

### Creating Groups and Assigning Images

Groups are collections of images that satellites replicate. Ground Control always uses its own `HARBOR_URL` as the source registry for a group. The optional `registry` field in the request is ignored. Create a group and add images:

```bash
curl -X POST $GC/api/groups/sync \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -d '{
    "group": "edge-images",
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

Assign the group to an existing satellite:

```bash
curl -X POST $GC/api/groups/satellite \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -d '{"satellite": "edge-01", "group": "edge-images"}'
```

### Verifying Replication

After assigning a group, the satellite replicates content on its next state sync (default: every 30 seconds, set by `state_replication_interval` in the satellite config). Check the satellite logs and verify the local OCI image layout (with `CONFIG_DIR=/data` in a container, the layout is at `/data/oci`):

```bash
test -f ~/.config/satellite/oci/oci-layout
jq '.manifests[].annotations["org.opencontainers.image.ref.name"]' \
    ~/.config/satellite/oci/index.json
```

## Delivering Images to Workloads

The default OCI layout is storage, not a registry endpoint. Satellite does not yet serve it over the OCI Distribution API; the transparent proxy is proposed in ADR-0009. Workloads get images in one of two ways today.

### BYO Registry with CRI Mirroring

With `--byo-registry --registry-url`, Satellite replicates into an external registry you run at the edge, and can point container runtimes at it as a mirror. Without BYO mode, `--mirrors` is ignored with a warning because there is no endpoint to mirror to.

```bash
# containerd: mirror docker.io and quay.io
./harbor-satellite --byo-registry --registry-url registry.edge:5000 \
  --mirrors=containerd:docker.io,quay.io ...

# Docker: mirror docker.io (only registry Docker supports mirroring)
./harbor-satellite --byo-registry --registry-url registry.edge:5000 \
  --mirrors=docker:true ...

# Podman
./harbor-satellite --byo-registry --registry-url registry.edge:5000 \
  --mirrors=podman:docker.io ...

# CRI-O
./harbor-satellite --byo-registry --registry-url registry.edge:5000 \
  --mirrors=crio:docker.io,quay.io ...
```

Files written:

- containerd: `/etc/containerd/certs.d/<registry>/hosts.toml`, plus `config_path` in `/etc/containerd/config.toml`
- Docker: `registry-mirrors` in `/etc/docker/daemon.json`
- CRI-O and Podman: `/etc/containers/registries.conf`

Notes:

- CRI config changes require root (satellite modifies system config files). Each file is backed up before it is changed
- Docker requires a service restart after config changes
- Multiple `--mirrors` flags can be combined
- `--fallback-only` applies the CRI configs and exits without starting the satellite

### Direct Delivery to k3s/RKE2 (Experimental)

With `--direct-delivery`, Satellite pulls each assigned image from Harbor and writes it as a tarball into the k3s or RKE2 agent images directory, which the node imports automatically. The directory is detected from `/var/lib/rancher/k3s/agent/images` or `/var/lib/rancher/rke2/agent/images`, or set with `--image-dir`:

```bash
sudo ./harbor-satellite --direct-delivery \
  --ground-control-url https://gc.example.com:8080 \
  --harbor-registry-url https://harbor.example.com \
  --spiffe-enabled
```
