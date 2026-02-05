# SSH PoP Attestation

Agents authenticate using SSH host certificates signed by a trusted SSH CA. Useful in environments that already use SSH certificate infrastructure.

The SSH CA public key is trusted by the SPIRE server, and each agent presents a host certificate signed by that CA.

## Prerequisites

- Docker and docker compose installed
- Harbor running (or use SKIP_HARBOR_HEALTH_CHECK=true for testing)
- ssh-keygen installed (for SSH CA and host certificate generation)

## Satellite Naming

The satellite name is derived from the SPIFFE ID path:
- `spiffe://<trust-domain>/satellite/edge-01` registers the satellite as `edge-01`
- `spiffe://<trust-domain>/satellite` registers the satellite as `default`

## Step 1: Start Ground Control with External SPIRE

### 1.1 Generate SSH certificates

This generates: SSH CA key pair, bootstrap trust bundle, and per-agent host certificates.

```bash
cd external/gc
./generate-certs.sh
```

Or manually:
```bash
mkdir -p certs

# SSH CA key pair
ssh-keygen -t ed25519 -f certs/ssh-ca -N "" -C "harbor-satellite-ssh-ca"

# Bootstrap trust bundle (self-signed X.509 for SPIRE upstream CA)
openssl genrsa -out certs/bootstrap.key 4096
openssl req -new -x509 -days 365 -key certs/bootstrap.key -out certs/bootstrap.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE Bootstrap CA"

# Agent host key + certificate (repeat for each agent)
ssh-keygen -t ed25519 -f certs/agent-gc-host-key -N "" -C "agent-gc"
ssh-keygen -s certs/ssh-ca -I "agent-gc" -h -n "spire-agent-gc" \
    -V "+52w" certs/agent-gc-host-key.pub
```

> NOTE: The bootstrap trust bundle uses a self-signed X.509 certificate as the SPIRE server's
> upstream CA. This is the standard SPIRE quickstart approach and is architecturally correct
> since SPIRE itself acts as the CA that issues short-lived, auto-rotated SVIDs to workloads.
> The self-signed cert is only used to seed SPIRE's internal CA chain.
>
> For production deployments, replace the `UpstreamAuthority "disk"` plugin with a
> proper CA backend (AWS PCA, HashiCorp Vault, cert-manager, or nested SPIRE) and
> store the signing key in an HSM or KMS. The private key material on disk should be
> considered extremely sensitive. See the
> [SPIRE UpstreamAuthority documentation](https://github.com/spiffe/spire/blob/main/doc/plugin_server_upstreamauthority_disk.md)
> for details.

### 1.2 Start SPIRE server and PostgreSQL

```bash
docker compose up -d postgres spire-server
```

### 1.3 Wait for SPIRE server to be healthy

```bash
docker exec spire-server /opt/spire/bin/spire-server healthcheck \
    -socketPath /tmp/spire-server/private/api.sock
```

### 1.4 Start SPIRE agent for GC

Agents auto-attest using their SSH host certificate. No token needed.

```bash
docker compose up -d spire-agent-gc
```

### 1.5 Register GC workload

The sshpop attestor assigns agent SPIFFE IDs based on the SSH public key fingerprint
(not the certificate identity), so the parentID must be extracted dynamically after
the agent attests.

```bash
# Get the actual agent SPIFFE ID
GC_AGENT_ID=$(docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock 2>/dev/null \
    | grep "SPIFFE ID" | grep "sshpop" | head -1 | awk '{print $NF}')

echo "GC agent SPIFFE ID: $GC_AGENT_ID"

docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID "$GC_AGENT_ID" \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock
```

### 1.6 Start Ground Control

```bash
docker compose up -d ground-control
```

### 1.7 Verify

```bash
curl -sk https://localhost:9080/ping
```

## Step 2: Start Satellite with External SPIRE

### 2.1 Start SPIRE agent for Satellite

The satellite agent must start and attest before registering the workload entry,
since the parentID is derived from the agent's SSH key fingerprint.

```bash
cd ../sat
docker compose up -d spire-agent-satellite
```

Wait for the agent to be healthy:
```bash
docker exec spire-agent-satellite /opt/spire/bin/spire-agent healthcheck \
    -socketPath /run/spire/sockets/agent.sock
```

### 2.2 Register satellite via Ground Control

Register the satellite using the GC API. For sshpop, you must first discover the agent SPIFFE ID
via the agents API, then pass it as `parent_agent_id`.

```bash
# Login to Ground Control
LOGIN_RESP=$(curl -sk -X POST https://localhost:9080/login \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"Harbor12345"}')
AUTH_TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# Discover the sshpop agent SPIFFE ID
AGENTS_RESP=$(curl -sk "https://localhost:9080/api/spire/agents?attestation_type=sshpop" \
    -H "Authorization: Bearer ${AUTH_TOKEN}")
SAT_AGENT_ID=$(echo "$AGENTS_RESP" | grep -o '"spiffe_id":"[^"]*"' | tail -1 | cut -d'"' -f4)

echo "Satellite agent SPIFFE ID: $SAT_AGENT_ID"

# Register satellite with explicit parent_agent_id
curl -sk -X POST https://localhost:9080/api/satellites/register \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{
      \"satellite_name\": \"edge-01\",
      \"selectors\": [\"docker:label:com.docker.compose.service:satellite\"],
      \"attestation_method\": \"sshpop\",
      \"parent_agent_id\": \"${SAT_AGENT_ID}\"
    }"
```

The API creates the SPIRE workload entry, satellite DB record, and robot account.

### 2.3 Start Satellite

```bash
docker compose up -d satellite
```

### 2.4 Verify

```bash
docker logs satellite
docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock
```

## Automated Setup

```bash
cd external/gc && ./setup.sh
cd ../sat && ./setup.sh
```

## Adding More Satellites

To spin up an additional satellite (e.g., `edge-02`), follow these steps from the `external/` directory.
The GC setup and first satellite must already be running.

### 1. Generate SSH host key for the new agent

```bash
cd gc/certs

# Temporarily fix CA key permissions for signing
chmod 600 ssh-ca

ssh-keygen -t ed25519 -f agent-satellite-2-host-key -N "" -C "agent-satellite-2"
ssh-keygen -s ssh-ca -I "agent-satellite-2" -h -n "spire-agent-satellite-2" \
    -V "+52w" agent-satellite-2-host-key.pub

# Restore permissions
chmod 644 ssh-ca
```

### 2. Start the agent and register via GC API

A compose override file `docker-compose.edge-02.yml` is provided in `sat/`.
It defines `spire-agent-satellite-2` and `satellite-2` services (zot on port 5051).

```bash
cd ../sat

# Start the SPIRE agent
docker compose -f docker-compose.yml -f docker-compose.edge-02.yml up -d spire-agent-satellite-2

# Wait for attestation
docker exec spire-agent-satellite-2 /opt/spire/bin/spire-agent healthcheck \
    -socketPath /run/spire/sockets/agent.sock

# Login to Ground Control
LOGIN_RESP=$(curl -sk -X POST https://localhost:9080/login \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"Harbor12345"}')
AUTH_TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# Discover the new sshpop agent SPIFFE ID
AGENTS_RESP=$(curl -sk "https://localhost:9080/api/spire/agents?attestation_type=sshpop" \
    -H "Authorization: Bearer ${AUTH_TOKEN}")
SAT2_AGENT_ID=$(echo "$AGENTS_RESP" | grep -o '"spiffe_id":"[^"]*"' | tail -1 | cut -d'"' -f4)

echo "Agent SPIFFE ID: $SAT2_AGENT_ID"

# Register satellite via GC API
curl -sk -X POST https://localhost:9080/api/satellites/register \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{
      \"satellite_name\": \"edge-02\",
      \"selectors\": [\"docker:label:com.docker.compose.service:satellite-2\"],
      \"attestation_method\": \"sshpop\",
      \"parent_agent_id\": \"${SAT2_AGENT_ID}\"
    }"
```

### 3. Start the satellite

```bash
docker compose -f docker-compose.yml -f docker-compose.edge-02.yml up -d satellite-2
```

### 4. Verify

```bash
docker logs satellite-2
# edge-02 zot registry is on port 5051
curl http://localhost:5051/v2/_catalog
```

The same pattern applies for any additional satellite: generate a host key, sign it with the
SSH CA, add compose services with unique container names and port mappings, start the agent,
register the workload with a unique SPIFFE ID (`/satellite/<name>`), and start the satellite.

## Cleanup

```bash
cd external/sat && ./cleanup.sh
cd ../gc && ./cleanup.sh
```
