# Quickstart

This guide walks you through deploying Harbor Satellite end-to-end with SPIFFE/SPIRE zero-trust identity. By the end, you will have:

- A Harbor registry with images
- A SPIRE server issuing identities
- Ground Control managing the fleet
- A satellite at the "edge" pulling images automatically

Everything runs locally with Docker Compose.

## Prerequisites

- Docker and Docker Compose installed
- `curl` and `jq` available
- A Harbor instance running with at least one image pushed (e.g., `library/nginx:alpine`)

If you do not have Harbor running, the quickest option is the [Harbor online installer](https://goharbor.io/docs/latest/install-config/). For a minimal local setup:

```bash
# Download and install Harbor (adjust version as needed)
wget https://github.com/goharbor/harbor/releases/download/v2.12.2/harbor-online-installer-v2.12.2.tgz
tar xzf harbor-online-installer-v2.12.2.tgz
cd harbor
cp harbor.yml.tmpl harbor.yml
# Edit harbor.yml: set hostname to your IP, disable HTTPS for local dev
./install.sh
```

After Harbor is running, push a test image:
```bash
docker pull nginx:alpine
docker tag nginx:alpine <your-harbor-host>/library/nginx:alpine
docker login <your-harbor-host>
docker push <your-harbor-host>/library/nginx:alpine
```

## Environment Variables

The quickstart uses these defaults. Override them if your Harbor setup differs:

| Variable | Default | Description |
|----------|---------|-------------|
| `HARBOR_URL` | `http://localhost:8080` | Harbor registry URL (containers use `host.docker.internal` internally) |
| `HARBOR_USERNAME` | `admin` | Harbor admin username |
| `HARBOR_PASSWORD` | `Harbor12345` | Harbor admin password |
| `ADMIN_PASSWORD` | `Harbor12345` | Ground Control admin password |
| `GC_HOST_PORT` | `9080` | Ground Control host port |

To override, export before running commands:
```bash
export HARBOR_URL=http://my-harbor:8080
export HARBOR_PASSWORD=MyPassword123
```

## Overview

You will set up two environments:

```
Cloud (gc/ directory):                Edge (sat/ directory):
  PostgreSQL                            SPIRE Agent (satellite)
  SPIRE Server                          Satellite (with embedded Zot registry)
  SPIRE Agent (ground control)
  Ground Control
```

The quickstart files live in:
```
deploy/quickstart/spiffe/join-token/external/
  gc/       <-- Cloud-side components
  sat/      <-- Edge-side components
```

## Step 1: Start the Cloud Side

### 1.1 Generate CA Certificates

```bash
cd deploy/quickstart/spiffe/join-token/external/gc
./generate-certs.sh
```

This creates a self-signed CA certificate that SPIRE uses to bootstrap trust.

### 1.2 Start PostgreSQL and SPIRE Server

```bash
docker compose up -d postgres spire-server
```

Wait for SPIRE server to be healthy:

```bash
docker exec spire-server /opt/spire/bin/spire-server healthcheck \
    -socketPath /tmp/spire-server/private/api.sock
```

### 1.3 Generate a Join Token for Ground Control's SPIRE Agent

```bash
GC_TOKEN=$(docker exec spire-server /opt/spire/bin/spire-server token generate \
    -spiffeID spiffe://harbor-satellite.local/agent/ground-control \
    -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')
echo "Token: $GC_TOKEN"
```

### 1.4 Create the SPIRE Agent Config

Create the agent config file with the token:

```bash
cat > spire/agent-gc-runtime.conf << EOF
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
    join_token = "$GC_TOKEN"
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }
    KeyManager "disk" {
        plugin_data {
            directory = "/opt/spire/data/agent"
        }
    }
    WorkloadAttestor "unix" {
        plugin_data {}
    }
    WorkloadAttestor "docker" {
        plugin_data {
            docker_socket_path = "unix:///var/run/docker.sock"
        }
    }
}

health_checks {
    listener_enabled = true
    bind_address = "0.0.0.0"
    bind_port = "8080"
    live_path = "/live"
    ready_path = "/ready"
}
EOF
```

### 1.5 Start the SPIRE Agent and Ground Control

```bash
docker compose up -d spire-agent-gc
```

Wait for the agent to attest, then register Ground Control as a workload:

```bash
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/ground-control \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock
```

Start Ground Control:

```bash
docker compose up -d ground-control --build
```

Verify it is running (HTTPS since SPIFFE is enabled):

```bash
curl -sk https://localhost:9080/ping
```

### 1.6 Automated Alternative

Instead of steps 1.1-1.5, you can run the setup script:

```bash
./setup.sh
```

## Step 2: Register a Satellite

### 2.1 Login to Ground Control

```bash
LOGIN_RESP=$(curl -sk -X POST https://localhost:9080/login \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"Harbor12345"}')
AUTH_TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
```

### 2.2 Register the Satellite

This single API call:
- Creates the satellite record in Ground Control
- Creates a SPIRE workload entry with the satellite's SPIFFE ID
- Generates a join token for the satellite's SPIRE agent
- Creates a robot account in Harbor

```bash
REGISTER_RESP=$(curl -sk -X POST https://localhost:9080/api/satellites/register \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
      "satellite_name": "edge-01",
      "region": "us-west",
      "selectors": ["docker:label:com.docker.compose.service:satellite"],
      "attestation_method": "join_token"
    }')
echo "$REGISTER_RESP" | jq .
```

Response:
```json
{
  "satellite": "edge-01",
  "region": "us-west",
  "spiffe_id": "spiffe://harbor-satellite.local/satellite/region/us-west/edge-01",
  "parent_agent_id": "spiffe://harbor-satellite.local/agent/edge-01",
  "join_token": "abc123...",
  "spire_server_address": "spire-server",
  "spire_server_port": 8081,
  "trust_domain": "harbor-satellite.local"
}
```

Save the join token:
```bash
SAT_TOKEN=$(echo "$REGISTER_RESP" | jq -r '.join_token')
```

## Step 3: Create a Group and Assign Images

### 3.1 Create a group with an image

```bash
curl -sk -X POST https://localhost:9080/api/groups/sync \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
      "group": "edge-images",
      "registry": "http://harbor:8080",
      "artifacts": [
        {
          "repository": "library/nginx",
          "tag": ["alpine"],
          "type": "image",
          "digest": "sha256:YOUR_DIGEST_HERE"
        }
      ]
    }'
```

To get the digest from Harbor, use the Harbor API:
```bash
DIGEST=$(curl -sk -u admin:Harbor12345 \
    -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
    "${HARBOR_URL:-http://localhost:8080}/v2/library/nginx/manifests/alpine" \
    -o /dev/null -w '' -D - | grep -i docker-content-digest | awk '{print $2}' | tr -d '\r')
echo "Digest: $DIGEST"
```

Then replace `YOUR_DIGEST_HERE` in the command above with the digest value.

### 3.2 Assign the group to the satellite

```bash
curl -sk -X POST https://localhost:9080/api/groups/satellite \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"satellite": "edge-01", "group": "edge-images"}'
```

Now Ground Control knows that `edge-01` should have all images in the `edge-images` group.

## Step 4: Start the Satellite

### 4.1 Create the satellite SPIRE agent config

Navigate to the satellite directory:
```bash
cd deploy/quickstart/spiffe/join-token/external/sat

cat > spire/agent-satellite-runtime.conf << EOF
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
    join_token = "$SAT_TOKEN"
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }
    KeyManager "disk" {
        plugin_data {
            directory = "/opt/spire/data/agent"
        }
    }
    WorkloadAttestor "unix" {
        plugin_data {}
    }
    WorkloadAttestor "docker" {
        plugin_data {
            docker_socket_path = "unix:///var/run/docker.sock"
        }
    }
}

health_checks {
    listener_enabled = true
    bind_address = "0.0.0.0"
    bind_port = "8080"
    live_path = "/live"
    ready_path = "/ready"
}
EOF
```

### 4.2 Start the SPIRE agent and satellite

```bash
docker compose up -d spire-agent-satellite
# Wait for agent to attest
sleep 15
docker compose up -d satellite --build
```

### 4.3 Automated alternative

```bash
./setup.sh
```

Note: The `setup.sh` scripts handle SPIRE agent setup and satellite launch. You still need to create groups and assign images (Step 3) manually. Without groups, the satellite will register but have no images to replicate.

## Step 5: Verify

### Check satellite logs

```bash
docker logs satellite
```

You should see:
1. SPIFFE connection to the local SPIRE agent
2. Successful Zero-Touch Registration (ZTR) with Ground Control
3. State fetching and image replication beginning

### Pull from the satellite's local registry

The satellite exposes its Zot registry on host port 5050 (mapped from container port 8585, as shown in the [architecture](architecture.md) config). Docker trusts localhost by default for plain HTTP:

```bash
# Using Docker (localhost is trusted for HTTP by default)
docker pull localhost:5050/library/nginx:alpine

# Using Podman
podman pull localhost:5050/library/nginx:alpine --tls-verify=false

# Using crane (for quick verification)
crane catalog localhost:5050
```

### Check SPIRE agents

```bash
docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock
```

You should see two agents: one for Ground Control and one for the satellite.

### Check satellite status in Ground Control

```bash
curl -sk https://localhost:9080/api/satellites \
    -H "Authorization: Bearer ${AUTH_TOKEN}" | jq .
```

## What Just Happened?

Here is what happened end to end:

1. **SPIRE server** started and became the trust authority for `harbor-satellite.local`
2. **Ground Control's SPIRE agent** attested with a join token, got its identity
3. **Ground Control** started, connected to its SPIRE agent, got its SVID (`spiffe://harbor-satellite.local/ground-control`)
4. **You registered a satellite** - GC created a SPIRE workload entry, join token, and Harbor robot account
5. **You created a group** with `nginx:alpine` and assigned it to the satellite
6. **Satellite's SPIRE agent** attested with its join token, got its identity
7. **Satellite** started, connected to its SPIRE agent, got its SVID (`spiffe://harbor-satellite.local/satellite/region/us-west/edge-01`)
8. **Satellite** sent an mTLS request to Ground Control's `/satellites/spiffe-ztr` endpoint
9. **Ground Control** verified the SVID, created robot credentials, returned the state URL
10. **Satellite** used the robot credentials to pull its state from Harbor
11. **Satellite** saw `nginx:alpine` in its desired state and replicated it to local Zot
12. **Satellite** now serves `nginx:alpine` locally on port 5050

The only secret transported to the edge was a one-time SPIRE join token (Step 2.2), which was invalidated after first use. After that, all identity and credentials were handled automatically: SVID from SPIRE, robot credentials from Ground Control over mTLS.

## Cleanup

Satellite side first (it depends on the GC network):

```bash
cd deploy/quickstart/spiffe/join-token/external/sat
./cleanup.sh

cd ../gc
./cleanup.sh
```

## Next Steps

- Read the [Architecture](architecture.md) doc for the full flow details
- Try [X.509 PoP attestation](../../deploy/quickstart/spiffe/x509pop/README.md) for production PKI
- Try [SSH PoP attestation](../../deploy/quickstart/spiffe/sshpop/README.md) for SSH-based environments
