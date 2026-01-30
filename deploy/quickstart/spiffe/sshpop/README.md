# SSH PoP Attestation

Agents authenticate using SSH host certificates signed by a trusted SSH CA. Useful in environments that already use SSH certificate infrastructure.

The SSH CA public key is trusted by the SPIRE server, and each agent presents a host certificate signed by that CA.

## Prerequisites

- Docker and docker compose installed
- Harbor running (or use SKIP_HARBOR_HEALTH_CHECK=true for testing)
- ssh-keygen installed (for SSH CA and host certificate generation)

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

# Bootstrap trust bundle (self-signed X.509)
openssl genrsa -out certs/bootstrap.key 4096
openssl req -new -x509 -days 365 -key certs/bootstrap.key -out certs/bootstrap.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE Bootstrap CA"

# Agent host key + certificate (repeat for each agent)
ssh-keygen -t ed25519 -f certs/agent-gc-host-key -N "" -C "agent-gc"
ssh-keygen -s certs/ssh-ca -I "agent-gc" -h -n "spire-agent-gc" \
    -V "+52w" certs/agent-gc-host-key.pub
```

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

The parentID for sshpop-attested agents follows the pattern:
`spiffe://<trust-domain>/spire/agent/sshpop/<certificate-identity>`

```bash
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/spire/agent/sshpop/agent-gc \
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
curl http://localhost:8080/ping
```

## Step 2: Start Satellite with External SPIRE

### 2.1 Register satellite workload

```bash
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/spire/agent/sshpop/agent-satellite \
    -spiffeID spiffe://harbor-satellite.local/satellite \
    -selector docker:label:com.docker.compose.service:satellite \
    -socketPath /tmp/spire-server/private/api.sock
```

### 2.2 Start SPIRE agent and Satellite

The satellite agent uses the SSH host certificate generated in step 1.1.

```bash
cd ../sat
docker compose up -d spire-agent-satellite
docker compose up -d satellite
```

### 2.3 Verify

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

## Cleanup

```bash
cd external/sat && ./cleanup.sh
cd ../gc && ./cleanup.sh
```
