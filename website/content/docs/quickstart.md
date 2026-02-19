---
title: "Quickstart"
weight: 4
---

This guide walks you through deploying Harbor Satellite end-to-end with SPIFFE/SPIRE zero-trust identity. By the end, you will have:

- A Harbor registry with images
- A SPIRE server issuing identities
- Ground Control managing the fleet
- A satellite at the "edge" pulling images automatically

Everything runs locally with Docker Compose. No need to clone the repository.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/) (v2+)
- [curl](https://curl.se/) - HTTP client for API calls
- [jq](https://jqlang.github.io/jq/download/) - JSON processor for parsing API responses
- [openssl](https://www.openssl.org/) - for generating CA certificates
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

You will set up two directories:

```text
quickstart/
  gc/                              <-- Cloud-side components
    docker-compose.yml
    certs/                         <-- Generated certificates (CA + x509pop CA + agent certs)
    spire/
      server.conf
      agent-gc.conf
  sat/                             <-- Edge-side components
    docker-compose.yml
    certs/                         <-- Copied from cloud (ca.crt, agent-satellite.crt, agent-satellite.key)
    agent-satellite.conf
```

## Step 1: Start the Cloud Side

{{< callout type="info" >}}
Run all commands in this step on your **cloud server**.
{{< /callout >}}

### 1.1 Create the directory structure

```bash
mkdir -p quickstart/gc/spire quickstart/sat
cd quickstart/gc
```

### 1.2 Generate Certificates

Generate the SPIRE upstream authority CA, X.509 PoP CA (signs agent certificates), and per-agent leaf certificates:

```bash
mkdir -p certs

# SPIRE upstream authority CA
openssl genrsa -out certs/ca.key 4096
openssl req -new -x509 -days 365 -key certs/ca.key -out certs/ca.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE CA"

# X.509 PoP CA (signs agent certificates for attestation)
openssl genrsa -out certs/x509pop-ca.key 4096
openssl req -new -x509 -days 365 -key certs/x509pop-ca.key -out certs/x509pop-ca.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=X509 PoP CA"

# Ground Control agent certificate
openssl genrsa -out certs/agent-gc.key 2048
openssl req -new -key certs/agent-gc.key -out certs/agent-gc.csr \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=agent-gc"
cat > certs/agent-gc.ext << 'EXTEOF'
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names
[alt_names]
URI.1 = spiffe://harbor-satellite.local/agent/ground-control
EXTEOF
openssl x509 -req -days 365 -in certs/agent-gc.csr \
    -CA certs/x509pop-ca.crt -CAkey certs/x509pop-ca.key -CAcreateserial \
    -out certs/agent-gc.crt -extfile certs/agent-gc.ext

# Satellite agent certificate (CN must match satellite_name used during registration)
openssl genrsa -out certs/agent-satellite.key 2048
openssl req -new -key certs/agent-satellite.key -out certs/agent-satellite.csr \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=edge-01"
cat > certs/agent-satellite.ext << 'EXTEOF'
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names
[alt_names]
URI.1 = spiffe://harbor-satellite.local/agent/satellite
EXTEOF
openssl x509 -req -days 365 -in certs/agent-satellite.csr \
    -CA certs/x509pop-ca.crt -CAkey certs/x509pop-ca.key -CAcreateserial \
    -out certs/agent-satellite.crt -extfile certs/agent-satellite.ext

# Cleanup temp files
rm -f certs/*.csr certs/*.ext certs/*.srl
chmod 644 certs/*.key certs/*.crt
```

### 1.3 Create the SPIRE Server Config

Create `spire/server.conf`. The server uses `NodeAttestor "x509pop"` so agents authenticate with pre-provisioned certificates instead of one-time tokens:

```bash
cat > spire/server.conf << 'EOF'
server {
    bind_address = "0.0.0.0"
    bind_port = "8081"
    socket_path = "/tmp/spire-server/private/api.sock"
    trust_domain = "harbor-satellite.local"
    data_dir = "/opt/spire/data/server"
    log_level = "INFO"
    ca_ttl = "24h"
    default_x509_svid_ttl = "1h"
    default_jwt_svid_ttl = "5m"
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "sqlite3"
            connection_string = "/opt/spire/data/server/datastore.sqlite3"
        }
    }
    NodeAttestor "x509pop" {
        plugin_data {
            ca_bundle_path = "/opt/spire/conf/server/x509pop-ca.crt"
        }
    }
    KeyManager "disk" {
        plugin_data {
            keys_path = "/opt/spire/data/server/keys.json"
        }
    }
    UpstreamAuthority "disk" {
        plugin_data {
            key_file_path = "/opt/spire/conf/server/ca.key"
            cert_file_path = "/opt/spire/conf/server/ca.crt"
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

### 1.4 Create the SPIRE Agent Config for Ground Control

Create `spire/agent-gc.conf`. This is a static config file with no tokens. The agent authenticates using its X.509 certificate:

```bash
cat > spire/agent-gc.conf << 'EOF'
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
}

plugins {
    NodeAttestor "x509pop" {
        plugin_data {
            private_key_path = "/opt/spire/conf/agent/agent.key"
            certificate_path = "/opt/spire/conf/agent/agent.crt"
        }
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

### 1.5 Create the Docker Compose file

Create `docker-compose.yml` in the `gc/` directory:

{{< details summary="gc/docker-compose.yml (click to expand)" >}}
```yaml
services:
  postgres:
    image: postgres:15-alpine
    container_name: harbor-satellite-postgres
    environment:
      POSTGRES_USER: harbor
      POSTGRES_PASSWORD: harbor
      POSTGRES_DB: harbor_satellite
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "harbor", "-d", "harbor_satellite"]
      interval: 5s
      timeout: 5s
      retries: 5
      start_period: 10s
    networks:
      - harbor-satellite

  spire-server:
    image: ghcr.io/spiffe/spire-server:1.12.3
    container_name: spire-server
    hostname: spire-server
    command: ["-config", "/opt/spire/conf/server/server.conf"]
    volumes:
      - ./spire/server.conf:/opt/spire/conf/server/server.conf:ro
      - ./certs/ca.crt:/opt/spire/conf/server/ca.crt:ro
      - ./certs/ca.key:/opt/spire/conf/server/ca.key:ro
      - ./certs/x509pop-ca.crt:/opt/spire/conf/server/x509pop-ca.crt:ro
      - spire-server-data:/opt/spire/data/server
      - spire-server-socket:/tmp/spire-server/private
    ports:
      - "${SPIRE_HOST_PORT:-9081}:8081"
    healthcheck:
      test: ["CMD", "/opt/spire/bin/spire-server", "healthcheck", "-socketPath", "/tmp/spire-server/private/api.sock"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    networks:
      - harbor-satellite

  spire-agent-gc:
    image: ghcr.io/spiffe/spire-agent:1.12.3
    container_name: spire-agent-gc
    hostname: spire-agent-gc
    pid: host
    command: ["-config", "/opt/spire/conf/agent/agent.conf"]
    volumes:
      - ./spire/agent-gc.conf:/opt/spire/conf/agent/agent.conf:ro
      - ./certs/ca.crt:/opt/spire/conf/agent/bootstrap.crt:ro
      - ./certs/agent-gc.crt:/opt/spire/conf/agent/agent.crt:ro
      - ./certs/agent-gc.key:/opt/spire/conf/agent/agent.key:ro
      - spire-agent-gc-data:/opt/spire/data/agent
      - spire-agent-gc-socket:/run/spire/sockets
      - ${DOCKER_SOCK:-/var/run/docker.sock}:/var/run/docker.sock:ro
    depends_on:
      spire-server:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "/opt/spire/bin/spire-agent", "healthcheck", "-socketPath", "/run/spire/sockets/agent.sock"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    networks:
      - harbor-satellite

  ground-control:
    image: registry.goharbor.io/harbor-satellite/ground-control:latest
    container_name: ground-control
    environment:
      - DB_HOST=postgres
      - DB_PORT=5432
      - DB_DATABASE=harbor_satellite
      - DB_USERNAME=harbor
      - DB_PASSWORD=harbor
      - PORT=8080
      - APP_ENV=development
      - HARBOR_URL=${HARBOR_URL:-http://host.docker.internal:8080}
      - HARBOR_USERNAME=${HARBOR_USERNAME:-admin}
      - HARBOR_PASSWORD=${HARBOR_PASSWORD:-Harbor12345}
      - SKIP_HARBOR_HEALTH_CHECK=${SKIP_HARBOR_HEALTH_CHECK:-false}
      - ADMIN_PASSWORD=${ADMIN_PASSWORD:-Harbor12345}
      - SPIFFE_ENABLED=true
      - SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock
      - SPIFFE_TRUST_DOMAIN=harbor-satellite.local
      - SPIRE_SERVER_SOCKET=/tmp/spire-server/private/api.sock
      - SPIRE_SERVER_ADDRESS=spire-server
      - SPIRE_SERVER_PORT=8081
      - SPIRE_TRUST_DOMAIN=harbor-satellite.local
    volumes:
      - spire-agent-gc-socket:/run/spire/sockets:ro
      - spire-server-socket:/tmp/spire-server/private:ro
    ports:
      - "${GC_HOST_PORT:-9080}:8080"
    depends_on:
      postgres:
        condition: service_healthy
      spire-agent-gc:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-sfk", "https://localhost:8080/ping"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 15s
    networks:
      - harbor-satellite

volumes:
  postgres-data:
  spire-server-data:
  spire-server-socket:
  spire-agent-gc-data:
  spire-agent-gc-socket:

networks:
  harbor-satellite:
    name: harbor-satellite
```
{{< /details >}}

### 1.6 Start PostgreSQL and SPIRE Server

```bash
docker compose up -d postgres spire-server
```

Wait for SPIRE server to be healthy:

```bash
docker exec spire-server /opt/spire/bin/spire-server healthcheck \
    -socketPath /tmp/spire-server/private/api.sock
```

### 1.7 Start the SPIRE Agent and Register Ground Control

Start the GC agent. It auto-attests using its X.509 certificate (no token needed):

```bash
docker compose up -d spire-agent-gc
```

Wait for the agent to attest, then discover its SPIFFE ID. With x509pop, the agent ID is based on the certificate fingerprint rather than a pre-defined path:

```bash
GC_AGENT_ID=$(docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock \
    | grep "SPIFFE ID" | grep "x509pop" | head -1 | awk '{print $NF}')
echo "GC agent ID: $GC_AGENT_ID"
```

Register Ground Control as a workload under this agent:

```bash
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID "$GC_AGENT_ID" \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock
```

### 1.8 Start Ground Control

```bash
docker compose up -d ground-control
```

Verify it is running (HTTPS since SPIFFE is enabled):

```bash
curl -sk https://localhost:9080/ping
```

## Step 2: Start the Satellite SPIRE Agent

{{< callout type="info" >}}
Run all commands in this step on your **edge device**. You will need the following files from the cloud server (generated in Step 1.2):
- `certs/ca.crt` (bootstrap trust bundle)
- `certs/agent-satellite.crt` (satellite agent certificate)
- `certs/agent-satellite.key` (satellite agent private key)
{{< /callout >}}

The satellite's SPIRE agent must be running and attested **before** you register the satellite in Ground Control. GC discovers the agent by matching the certificate CN against the satellite name.

### 2.1 Copy certificates from cloud

On the edge device, create the satellite directory and copy the certificates from the cloud server:

```bash
mkdir -p quickstart/sat/certs
cd quickstart/sat

# Copy these three files from your cloud server's quickstart/gc/certs/ directory
scp cloud-server:quickstart/gc/certs/ca.crt certs/
scp cloud-server:quickstart/gc/certs/agent-satellite.crt certs/
scp cloud-server:quickstart/gc/certs/agent-satellite.key certs/
```

### 2.2 Create the satellite SPIRE agent config

Create `agent-satellite.conf`. Like the GC agent, this uses x509pop attestation with no tokens:

```bash
cat > agent-satellite.conf << 'EOF'
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
}

plugins {
    NodeAttestor "x509pop" {
        plugin_data {
            private_key_path = "/opt/spire/conf/agent/agent.key"
            certificate_path = "/opt/spire/conf/agent/agent.crt"
        }
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

### 2.3 Create the Docker Compose file

Create `docker-compose.yml`:

{{< details summary="sat/docker-compose.yml (click to expand)" >}}
```yaml
services:
  spire-agent-satellite:
    image: ghcr.io/spiffe/spire-agent:1.12.3
    container_name: spire-agent-satellite
    hostname: spire-agent-satellite
    pid: host
    command: ["-config", "/opt/spire/conf/agent/agent.conf"]
    volumes:
      - ./agent-satellite.conf:/opt/spire/conf/agent/agent.conf:ro
      - ./certs/ca.crt:/opt/spire/conf/agent/bootstrap.crt:ro
      - ./certs/agent-satellite.crt:/opt/spire/conf/agent/agent.crt:ro
      - ./certs/agent-satellite.key:/opt/spire/conf/agent/agent.key:ro
      - spire-agent-satellite-data:/opt/spire/data/agent
      - spire-agent-satellite-socket:/run/spire/sockets
      - ${DOCKER_SOCK:-/var/run/docker.sock}:/var/run/docker.sock:ro
    healthcheck:
      test: ["CMD", "/opt/spire/bin/spire-agent", "healthcheck", "-socketPath", "/run/spire/sockets/agent.sock"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    networks:
      - harbor-satellite

  satellite:
    image: registry.goharbor.io/harbor-satellite/satellite:latest
    container_name: satellite
    environment:
      - GROUND_CONTROL_URL=https://ground-control:8080
      - USE_UNSECURE=true
      - SPIFFE_ENABLED=true
      - SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock
      - SPIFFE_EXPECTED_SERVER_ID=spiffe://harbor-satellite.local/ground-control
    volumes:
      - spire-agent-satellite-socket:/run/spire/sockets:ro
      - satellite-data:/data
    ports:
      - "${SATELLITE_ZOT_PORT:-5050}:8585"
    depends_on:
      spire-agent-satellite:
        condition: service_healthy
    networks:
      - harbor-satellite

volumes:
  spire-agent-satellite-data:
  spire-agent-satellite-socket:
  satellite-data:

networks:
  harbor-satellite:
    external: true
```
{{< /details >}}

### 2.4 Start the satellite SPIRE agent

Start only the SPIRE agent for now. The satellite container will be started after registration in Step 4.

```bash
docker compose up -d spire-agent-satellite
```

Wait for the agent to attest with the SPIRE server:

```bash
docker exec spire-agent-satellite /opt/spire/bin/spire-agent healthcheck \
    -socketPath /run/spire/sockets/agent.sock
```

## Step 3: Register Satellite and Create Groups

{{< callout type="info" >}}
Run all commands in this step on your **cloud server**. The satellite SPIRE agent from Step 2 must be running and attested before proceeding.
{{< /callout >}}

### 3.1 Login to Ground Control

```bash
LOGIN_RESP=$(curl -sk -X POST https://localhost:9080/login \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"Harbor12345"}')
AUTH_TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
```

### 3.2 Register the Satellite

This API call finds the attested satellite agent by matching `x509pop:subject:cn:edge-01` (the CN from the certificate generated in Step 1.2), then:

- Creates the satellite record in Ground Control
- Creates a SPIRE workload entry with the satellite's SPIFFE ID
- Creates a robot account in Harbor

```bash
curl -sk -X POST https://localhost:9080/api/satellites/register \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
      "satellite_name": "edge-01",
      "selectors": ["docker:label:com.docker.compose.service:satellite"],
      "attestation_method": "x509pop"
    }' | jq .
```

### 3.3 Create a group with an image

Note: The `registry` field uses the Docker-internal service name (`http://harbor:8080`), not your host-facing `HARBOR_URL`. Ground Control runs inside Docker and resolves `harbor` via the Compose network.

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
DIGEST=$(curl -sk -u "${HARBOR_USERNAME:-admin}:${HARBOR_PASSWORD:-Harbor12345}" \
    -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
    "${HARBOR_URL:-http://localhost:8080}/v2/library/nginx/manifests/alpine" \
    -o /dev/null -w '' -D - | grep -i docker-content-digest | awk '{print $2}' | tr -d '\r')
echo "Digest: $DIGEST"
```

Then replace `YOUR_DIGEST_HERE` in the command above with the digest value.

### 3.4 Assign the group to the satellite

```bash
curl -sk -X POST https://localhost:9080/api/groups/satellite \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"satellite": "edge-01", "group": "edge-images"}'
```

Now Ground Control knows that `edge-01` should have all images in the `edge-images` group.

## Step 4: Start the Satellite

{{< callout type="info" >}}
Run this on your **edge device**.
{{< /callout >}}

Now that the satellite is registered and has a group assigned, start the satellite container:

```bash
# From the sat/ directory
docker compose up -d satellite
```

## Step 5: Verify

### Check satellite logs (edge device)

```bash
docker logs satellite
```

You should see:

1. SPIFFE connection to the local SPIRE agent
2. Successful Zero-Touch Registration (ZTR) with Ground Control
3. State fetching and image replication beginning

### Pull from the satellite's local registry (edge device)

The satellite exposes its Zot registry on host port 5050 (mapped from container port 8585, as shown in the [architecture](architecture.md) config). Docker trusts localhost by default for plain HTTP:

```bash
# Using Docker (localhost is trusted for HTTP by default)
docker pull localhost:5050/library/nginx:alpine

# Using Podman
podman pull localhost:5050/library/nginx:alpine --tls-verify=false

# Using crane (for quick verification)
crane catalog localhost:5050
```

### Check SPIRE agents (cloud server)

```bash
docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock
```

You should see two agents: one for Ground Control and one for the satellite.

### Check satellite status in Ground Control (cloud server)

```bash
curl -sk https://localhost:9080/api/satellites \
    -H "Authorization: Bearer ${AUTH_TOKEN}" | jq .
```

## What Just Happened?

Here is what happened end to end:

1. **You generated X.509 certificates** signed by the x509pop CA for both agents (CN=agent-gc and CN=edge-01)
2. **SPIRE server** started and became the trust authority for `harbor-satellite.local`
3. **Ground Control's SPIRE agent** attested using its X.509 certificate (x509pop), got its identity
4. **Ground Control** started, connected to its SPIRE agent, got its SVID (`spiffe://harbor-satellite.local/ground-control`)
5. **Satellite's SPIRE agent** attested using its X.509 certificate (x509pop), got its identity
6. **You registered a satellite** via the GC API. GC found the attested agent by matching `x509pop:subject:cn:edge-01`, created a SPIRE workload entry and Harbor robot account
7. **You created a group** with `nginx:alpine` and assigned it to the satellite
8. **Satellite** started, connected to its SPIRE agent, got its SVID
9. **Satellite** sent an mTLS request to Ground Control's `/satellites/spiffe-ztr` endpoint
10. **Ground Control** verified the SVID, created robot credentials, returned the state URL
11. **Satellite** used the robot credentials to pull its state from Harbor
12. **Satellite** saw `nginx:alpine` in its desired state and replicated it to local Zot
13. **Satellite** now serves `nginx:alpine` locally on port 5050

No runtime tokens were used. The only secrets transported to the edge were the X.509 agent certificate and key (Step 4.1), which can be pre-provisioned during device setup. After attestation, all credentials are handled automatically via SPIRE SVIDs and mTLS.

## Cleanup

On the **edge device** first (it depends on the GC network):

```bash
# From sat/ directory
docker compose down -v --remove-orphans
```

Then on the **cloud server**:

```bash
# From gc/ directory
docker compose down -v --remove-orphans
docker network rm harbor-satellite 2>/dev/null || true
rm -rf certs
```

## Next Steps

- Read the [Architecture](architecture.md) doc for the full flow details
- Try [SSH PoP attestation](https://github.com/container-registry/harbor-satellite/tree/main/deploy/quickstart/spiffe/sshpop) for SSH certificate-based environments
- Try [join token attestation](https://github.com/container-registry/harbor-satellite/tree/main/deploy/quickstart/spiffe/join-token) for simpler development setups
