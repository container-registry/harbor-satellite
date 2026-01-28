#!/bin/bash
# Setup script for X.509 Certificate SPIFFE Authentication
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Harbor Satellite SPIFFE Quickstart (X.509 PoP) ==="
echo ""

# Step 1: Generate certificates
echo "[1/6] Generating X.509 certificates..."
./generate-certs.sh

# Step 2: Start infrastructure
echo "[2/6] Starting infrastructure services..."
docker compose up -d postgres mock-harbor spire-server
echo "Waiting for SPIRE server to be healthy..."
sleep 10

for i in {1..30}; do
    if docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock > /dev/null 2>&1; then
        echo "SPIRE server is healthy"
        break
    fi
    echo "Waiting for SPIRE server... ($i/30)"
    sleep 2
done

# Step 3: Start SPIRE agents (they will auto-attest with their certificates)
echo "[3/6] Starting SPIRE agents with X.509 PoP attestation..."
docker compose up -d spire-agent-gc spire-agent-satellite

echo "Waiting for agents to attest..."
sleep 15

# Verify agent attestation
echo "Verifying agent attestation..."
docker exec spire-server /opt/spire/bin/spire-server agent list -socketPath /tmp/spire-server/private/api.sock

# Step 4: Register workloads
echo "[4/6] Registering SPIFFE workloads..."

# Get the agent SPIFFE IDs from the certificate CNs
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/ground-control \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock || true

docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/satellite \
    -spiffeID spiffe://harbor-satellite.local/satellite \
    -selector docker:label:com.docker.compose.service:satellite \
    -socketPath /tmp/spire-server/private/api.sock || true

echo "Listing registered entries..."
docker exec spire-server /opt/spire/bin/spire-server entry show -socketPath /tmp/spire-server/private/api.sock

# Step 5: Start Ground Control
echo "[5/6] Starting Ground Control..."
docker compose up -d ground-control

echo "Waiting for Ground Control to be healthy..."
for i in {1..30}; do
    if curl -s http://localhost:8080/ping > /dev/null 2>&1; then
        echo "Ground Control is healthy"
        break
    fi
    echo "Waiting for Ground Control... ($i/30)"
    sleep 2
done

# Step 6: Start Satellite
echo "[6/6] Starting Satellite..."
docker compose up -d satellite

echo "Waiting for Satellite to initialize..."
sleep 10

# Verification
echo ""
echo "=== Verification ==="
echo "SPIRE Server Status:"
docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock

echo ""
echo "Registered Agents (X.509 PoP attested):"
docker exec spire-server /opt/spire/bin/spire-server agent list -socketPath /tmp/spire-server/private/api.sock

echo ""
echo "Ground Control Ping:"
curl -s http://localhost:8080/ping

echo ""
echo "=== Setup Complete ==="
echo ""
echo "X.509 PoP attestation uses pre-provisioned certificates for agent authentication."
echo "This method is suitable for environments where certificates can be securely"
echo "distributed to agents before deployment."
echo ""
echo "To view logs:"
echo "  docker compose logs -f satellite"
echo "  docker compose logs -f ground-control"
echo ""
echo "To tear down:"
echo "  ./cleanup.sh"
