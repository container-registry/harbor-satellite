# SSH PoP Attestation

Agents authenticate using SSH host certificates signed by a trusted SSH CA. Useful in environments that already use SSH certificate infrastructure.

The SSH CA public key is trusted by the SPIRE server, and each agent presents a host certificate signed by that CA.

## Prerequisites

- Docker and docker compose installed
- Harbor running (or set `SKIP_HARBOR_HEALTH_CHECK=true` for testing)
- ssh-keygen and OpenSSL installed (for the SSH CA, host certificates and the bootstrap CA)
- `HARBOR_*`, `ADMIN_PASSWORD` and `HARBOR_REGISTRY_URL` exported if you do not use the defaults. See [Environment Variables](../README.md#environment-variables).

## Satellite Naming

`POST /api/satellites/register` issues the workload SPIFFE ID `spiffe://<trust-domain>/satellite/region/<region>/<name>`. The region defaults to `default`, so `edge-01` gets `spiffe://harbor-satellite.local/satellite/region/default/edge-01`. Ground Control derives the satellite name from the last path segment.

## Step 1: Start Ground Control with External SPIRE

### 1.1 Generate SSH certificates

This generates: SSH CA key pair, bootstrap trust bundle, and per-agent host certificates.

```bash
cd external/gc
./generate-certs.sh
```

The script creates, in `certs/`:
- `ssh-ca`, `ssh-ca.pub`: SSH CA key pair trusted by the SPIRE server
- `bootstrap.key`, `bootstrap.crt`: self-signed X.509 CA that seeds the SPIRE server
- `agent-gc-host-key*`, `agent-satellite-host-key*`, `agent-satellite-2-host-key*`: host keys and CA-signed host certificates for the GC agent, the satellite agent and the optional second satellite ([Adding More Satellites](#adding-more-satellites))

To generate them by hand, run the commands in [`external/gc/generate-certs.sh`](external/gc/generate-certs.sh).

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

The sshpop attestor assigns agent SPIFFE IDs based on a SHA-256 hash of the agent's host
certificate (not the certificate identity), so the parentID is read from the server after
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

See [Health checks](../README.md#health-checks).

## Step 2: Start Satellite with External SPIRE

### 2.1 Start SPIRE agent for Satellite

The satellite agent must start and attest before registering the workload entry,
since the parentID is derived from the agent's host certificate.

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

Register the satellite using the GC API. For sshpop, pass the agent SPIFFE ID as `parent_agent_id`.
SPIRE derives it from the unpadded base64url SHA-256 of the whole host certificate blob, not from the
key fingerprint that `ssh-keygen -l` prints. You can also read it from
`spire-server agent list` once the agent has attested.

First [log in](../README.md#log-in) to get `AUTH_TOKEN`, then:

```bash
# Compute agent SPIFFE ID from the host certificate
SSH_FINGERPRINT=$(awk '{print $2}' ../gc/certs/agent-satellite-host-key-cert.pub \
    | openssl base64 -d -A | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
SAT_AGENT_ID="spiffe://harbor-satellite.local/spire/agent/sshpop/${SSH_FINGERPRINT}"
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

The API creates the SPIRE workload entry, satellite DB record and robot account, and assigns the `default` config.

### 2.3 Start Satellite

The satellite needs `HARBOR_REGISTRY_URL` (default `http://host.docker.internal:8080`).

```bash
docker compose up -d satellite
docker logs satellite
```

## Step 3: Groups, Configs and Verification

Continue with the [shared steps](../README.md#shared-steps): assign a group (and optionally a custom config) to `edge-01`, then [verify](../README.md#verify) replication.

## Automated Setup and Cleanup

See [Automated setup](../README.md#automated-setup) and [Cleanup](../README.md#cleanup) with `<method>` set to `sshpop`.

## Adding More Satellites

To spin up an additional satellite (e.g., `edge-02`), follow these steps from the `external/` directory.
The GC setup and first satellite must already be running.

### 1. Host key for the new agent

`generate-certs.sh` already created `gc/certs/agent-satellite-2-host-key` and its certificate `agent-satellite-2-host-key-cert.pub` for `edge-02`.

### 2. Start the agent and register via GC API

The compose override file `sat/docker-compose.edge-02.yml` defines the `spire-agent-satellite-2` and `satellite-2` services, with a separate persistent OCI store.

Run from the `external/` directory. First [log in](../README.md#log-in) to get `AUTH_TOKEN` if you have not already.

```bash
cd sat

# Compute agent SPIFFE ID from the host certificate
SSH_FINGERPRINT=$(awk '{print $2}' ../gc/certs/agent-satellite-2-host-key-cert.pub \
    | openssl base64 -d -A | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
SAT2_AGENT_ID="spiffe://harbor-satellite.local/spire/agent/sshpop/${SSH_FINGERPRINT}"
echo "Agent SPIFFE ID: $SAT2_AGENT_ID"

# Start the SPIRE agent
docker compose -f docker-compose.yml -f docker-compose.edge-02.yml up -d spire-agent-satellite-2

# Wait for attestation
docker exec spire-agent-satellite-2 /opt/spire/bin/spire-agent healthcheck \
    -socketPath /run/spire/sockets/agent.sock

# Register satellite via GC API (AUTH_TOKEN from the login step)
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
docker exec satellite-2 test -f /data/oci/oci-layout
```

`sat/cleanup.sh` removes the `edge-02` containers and volumes along with the first satellite.

The same pattern applies for any additional satellite: generate a host key, sign it with the
SSH CA, add compose services with unique container names and volumes, start the agent,
register it with a unique `satellite_name` (Ground Control issues `/satellite/region/<region>/<name>`),
and start the satellite. Assign groups to the new satellite as in the
[shared steps](../README.md#create-and-assign-a-group).
