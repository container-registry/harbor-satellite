# X.509 PoP Attestation

Agents authenticate using pre-provisioned X.509 certificates signed by a trusted CA. No runtime token exchange needed; agents auto-attest on startup.

Suitable for environments where certificates can be securely distributed before deployment.

## Prerequisites

- Docker and docker compose installed
- Harbor running (or use SKIP_HARBOR_HEALTH_CHECK=true for testing)
- OpenSSL installed (for certificate generation)

## Step 1: Start Ground Control with External SPIRE

### 1.1 Generate X.509 certificates

This generates: SPIRE upstream authority CA, X.509 PoP CA, and per-agent leaf certificates.

```bash
cd external/gc
./generate-certs.sh
```

Or manually:
```bash
mkdir -p certs

# SPIRE upstream authority CA
openssl genrsa -out certs/ca.key 4096
openssl req -new -x509 -days 365 -key certs/ca.key -out certs/ca.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE CA"

# X.509 PoP CA (signs agent certs)
openssl genrsa -out certs/x509pop-ca.key 4096
openssl req -new -x509 -days 365 -key certs/x509pop-ca.key -out certs/x509pop-ca.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=X509 PoP CA"

# Agent certificate (repeat for each agent)
openssl genrsa -out certs/agent-gc.key 2048
openssl req -new -key certs/agent-gc.key -out certs/agent-gc.csr \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=agent-gc"
# Sign with x509pop CA (add SPIFFE URI SAN)
openssl x509 -req -days 365 -in certs/agent-gc.csr \
    -CA certs/x509pop-ca.crt -CAkey certs/x509pop-ca.key -CAcreateserial \
    -out certs/agent-gc.crt
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

Agents auto-attest using their X.509 certificate. No token needed.

```bash
docker compose up -d spire-agent-gc
```

### 1.5 Register GC workload

```bash
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/ground-control \
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
    -parentID spiffe://harbor-satellite.local/agent/satellite \
    -spiffeID spiffe://harbor-satellite.local/satellite \
    -selector docker:label:com.docker.compose.service:satellite \
    -socketPath /tmp/spire-server/private/api.sock
```

### 2.2 Start SPIRE agent and Satellite

The satellite agent uses the certificate generated in step 1.1.

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
