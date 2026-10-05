#!/bin/bash
# Setup Ground Control with External SPIRE (X.509 PoP Attestation)
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Ground Control Setup (X.509 PoP) ==="
echo ""

# Step 1: Generate certificates
echo "[1/5] Generating X.509 certificates..."
./generate-certs.sh

# Step 2: Start infrastructure
echo "[2/5] Starting PostgreSQL and SPIRE server..."
docker compose up -d postgres spire-server
echo "Waiting for SPIRE server to be healthy..."

for i in $(seq 1 30); do
    if docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock > /dev/null 2>&1; then
        echo "SPIRE server is healthy"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "ERROR: SPIRE server failed to start"
        exit 1
    fi
    echo "Waiting for SPIRE server... ($i/30)"
    sleep 2
done

# Step 3: Start SPIRE agent (auto-attests with x509 certificate)
echo "[3/5] Starting SPIRE agent for Ground Control (X.509 PoP)..."
docker compose up -d spire-agent-gc

echo "Waiting for agent to attest..."
sleep 10

for i in $(seq 1 20); do
    if docker exec spire-agent-gc /opt/spire/bin/spire-agent healthcheck -socketPath /run/spire/sockets/agent.sock > /dev/null 2>&1; then
        echo "SPIRE agent is healthy"
        break
    fi
    if [ "$i" -eq 20 ]; then
        echo "ERROR: SPIRE agent failed to attest"
        exit 1
    fi
    echo "Waiting for SPIRE agent... ($i/20)"
    sleep 2
done

# Step 4: Register GC workload
# x509pop agents get SPIFFE IDs based on cert fingerprint, so we must
# extract the actual agent ID from the server after attestation.
echo "[4/5] Registering Ground Control workload..."
GC_AGENT_ID=$(docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock 2>/dev/null \
    | grep "SPIFFE ID" | grep "x509pop" | head -1 | awk '{print $NF}')

if [ -z "$GC_AGENT_ID" ]; then
    echo "ERROR: Could not find attested GC agent SPIFFE ID"
    exit 1
fi
echo "GC agent SPIFFE ID: $GC_AGENT_ID"

docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID "$GC_AGENT_ID" \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock || true

# Step 5: Start Ground Control
echo "[5/5] Starting Ground Control..."
docker compose up -d ground-control

echo "Waiting for Ground Control to be healthy..."
for i in $(seq 1 30); do
    if curl -sk https://localhost:${GC_HOST_PORT:-9080}/ping > /dev/null 2>&1; then
        echo "Ground Control is healthy"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "ERROR: Ground Control failed to start"
        exit 1
    fi
    echo "Waiting for Ground Control... ($i/30)"
    sleep 2
done

echo ""
echo "=== Ground Control Setup Complete ==="
echo ""
echo "Services running:"
echo "  PostgreSQL:      (internal only)"
echo "  SPIRE Server:    localhost:${SPIRE_HOST_PORT:-9081}"
echo "  Ground Control:  localhost:${GC_HOST_PORT:-9080}"
echo ""
echo "Next: Set up satellite in ../sat/"
echo "Cleanup: ./cleanup.sh"
