#!/bin/bash
# Setup Satellite with External SPIRE Agent (SSH PoP Attestation)
# Requires GC-side setup to be running (../gc/setup.sh)
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Satellite Setup (SSH PoP) ==="
echo ""

# Verify GC is running
echo "[1/4] Verifying Ground Control is running..."
if ! curl -sk https://localhost:${GC_HOST_PORT:-9080}/ping > /dev/null 2>&1; then
    echo "ERROR: Ground Control is not running. Run ../gc/setup.sh first."
    exit 1
fi
echo "Ground Control is reachable"

# Verify satellite SSH host key exists
if [ ! -f "../gc/certs/agent-satellite-host-key" ]; then
    echo "ERROR: Satellite agent host key not found. Run ../gc/generate-certs.sh first."
    exit 1
fi

# Start satellite agent first (must attest before we can get its SPIFFE ID)
echo "[2/4] Starting SPIRE agent for Satellite (SSH PoP)..."
docker compose up -d spire-agent-satellite

echo "Waiting for satellite agent to attest..."
sleep 10

for i in $(seq 1 20); do
    if docker exec spire-agent-satellite /opt/spire/bin/spire-agent healthcheck -socketPath /run/spire/sockets/agent.sock > /dev/null 2>&1; then
        echo "Satellite SPIRE agent is healthy"
        break
    fi
    if [ "$i" -eq 20 ]; then
        echo "ERROR: Satellite SPIRE agent failed to attest"
        exit 1
    fi
    echo "Waiting for satellite SPIRE agent... ($i/20)"
    sleep 2
done

# Register satellite workload using actual agent SPIFFE ID
# sshpop agents get SPIFFE IDs based on SSH key fingerprint, so we must
# extract the actual satellite agent ID from the server after attestation.
echo "[3/4] Registering satellite workload..."
SAT_AGENT_ID=$(docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock 2>/dev/null \
    | grep "SPIFFE ID" | grep "sshpop" | tail -1 | awk '{print $NF}')

if [ -z "$SAT_AGENT_ID" ]; then
    echo "ERROR: Could not find attested satellite agent SPIFFE ID"
    exit 1
fi
echo "Satellite agent SPIFFE ID: $SAT_AGENT_ID"

docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID "$SAT_AGENT_ID" \
    -spiffeID spiffe://harbor-satellite.local/satellite/edge-01 \
    -selector docker:label:com.docker.compose.service:satellite \
    -socketPath /tmp/spire-server/private/api.sock || true

# Start satellite
echo "[4/4] Starting Satellite..."
docker compose up -d satellite

echo "Waiting for Satellite to initialize..."
sleep 5

echo ""
echo "=== Satellite Setup Complete ==="
echo ""
echo "Services running:"
echo "  Satellite SPIRE Agent: (internal)"
echo "  Satellite:             (internal)"
echo ""
echo "Verify with:"
echo "  docker logs satellite"
echo "  docker exec spire-server /opt/spire/bin/spire-server agent list -socketPath /tmp/spire-server/private/api.sock"
echo ""
echo "Cleanup: ./cleanup.sh"
