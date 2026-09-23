# X.509 PoP Attestation

Agents authenticate using pre-provisioned X.509 certificates signed by a trusted CA. No runtime token exchange needed; agents auto-attest on startup.

Suitable for environments where certificates can be securely distributed before deployment.

## Prerequisites

- Docker and docker compose installed
- Harbor running (or set `SKIP_HARBOR_HEALTH_CHECK=true` for testing)
- OpenSSL installed (for certificate generation)
- `HARBOR_*`, `ADMIN_PASSWORD` and `HARBOR_REGISTRY_URL` exported if you do not use the defaults. See [Environment Variables](../README.md#environment-variables).

## Step 1: Start Ground Control with External SPIRE

### 1.1 Generate X.509 certificates

This generates: SPIRE upstream authority CA, X.509 PoP CA, and per-agent leaf certificates.

```bash
cd external/gc
./generate-certs.sh
```

The script creates, in `certs/`:
- `ca.key`, `ca.crt`: SPIRE upstream authority CA
- `x509pop-ca.key`, `x509pop-ca.crt`: X.509 PoP CA that signs the agent certificates
- `agent-gc.key`, `agent-gc.crt`: GC agent certificate
- `agent-satellite.key`, `agent-satellite.crt`: satellite agent certificate with `CN=edge-01`

The satellite certificate CN must equal the `satellite_name` used in step 2.2, because Ground Control finds the attested agent by the `x509pop:subject:cn:<satellite_name>` selector. To generate the certificates by hand, run the commands in [`external/gc/generate-certs.sh`](external/gc/generate-certs.sh), including the SAN extension files.

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

The x509pop attestor assigns agent SPIFFE IDs based on certificate fingerprint:
`spiffe://<trust-domain>/spire/agent/x509pop/<fingerprint>`. Extract the actual
agent ID after attestation:

```bash
# Get the GC agent SPIFFE ID (assigned by x509pop attestor)
GC_AGENT_ID=$(docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock \
    | grep "SPIFFE ID" | grep "x509pop" | head -1 | awk '{print $NF}')

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

See [Health checks](../README.md#health-checks).

## Step 2: Start Satellite with External SPIRE

### 2.1 Start SPIRE agent for Satellite

The satellite agent uses the certificate generated in step 1.1. Start it first
so it can attest before registering the workload entry.

```bash
cd ../sat
docker compose up -d spire-agent-satellite
```

### 2.2 Register satellite via Ground Control

Register the satellite using the GC API. The API finds the attested x509pop agent
by matching the certificate CN (`edge-01`) and creates the workload entry.

First [log in](../README.md#log-in) to get `AUTH_TOKEN`, then:

```bash
# Register satellite (auto-matches x509pop agent by CN)
curl -sk -X POST https://localhost:9080/api/satellites/register \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
      "satellite_name": "edge-01",
      "selectors": ["docker:label:com.docker.compose.service:satellite"],
      "attestation_method": "x509pop"
    }'
```

The API creates the SPIRE workload entry, satellite DB record and robot account, and assigns the `default` config. The satellite's SPIFFE ID is `spiffe://harbor-satellite.local/satellite/region/default/edge-01`.

### 2.3 Start Satellite

The satellite needs `HARBOR_REGISTRY_URL` (default `http://host.docker.internal:8080`).

```bash
docker compose up -d satellite
docker logs satellite
```

## Step 3: Groups, Configs and Verification

Continue with the [shared steps](../README.md#shared-steps): assign a group (and optionally a custom config) to `edge-01`, then [verify](../README.md#verify) replication.

## Automated Setup and Cleanup

See [Automated setup](../README.md#automated-setup) and [Cleanup](../README.md#cleanup) with `<method>` set to `x509pop`.
