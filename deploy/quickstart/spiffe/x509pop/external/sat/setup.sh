#!/bin/bash
# Setup Satellite with External SPIRE Agent (X.509 PoP Attestation)
# Requires GC-side setup to be running (../gc/setup.sh)
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Satellite Setup (X.509 PoP) ==="
echo ""

# Verify GC is running
echo "[1/4] Verifying Ground Control is running..."
if ! curl -sk https://localhost:${GC_HOST_PORT:-9080}/ping > /dev/null 2>&1; then
    echo "ERROR: Ground Control is not running. Run ../gc/setup.sh first."
    exit 1
fi
echo "Ground Control is reachable"

# Verify satellite certificate exists
if [ ! -f "../gc/certs/agent-satellite.crt" ]; then
    echo "ERROR: Satellite agent certificate not found. Run ../gc/generate-certs.sh first."
    exit 1
fi

# Start satellite SPIRE agent first (must attest before we can register workload)
echo "[2/4] Starting satellite SPIRE agent..."
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

# Register satellite via GC API (auto-matches by CN selector)
echo "[3/4] Registering satellite workload via Ground Control..."

GC_URL="https://localhost:${GC_HOST_PORT:-9080}"

LOGIN_RESP=$(curl -sk -w "\n%{http_code}" -X POST "${GC_URL}/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD:-Harbor12345}\"}")
HTTP_CODE=$(echo "$LOGIN_RESP" | tail -1)
LOGIN_BODY=$(echo "$LOGIN_RESP" | sed '$d')

if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Login failed (HTTP $HTTP_CODE)"
    exit 1
fi

AUTH_TOKEN=$(echo "$LOGIN_BODY" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$AUTH_TOKEN" ]; then
    echo "ERROR: Failed to parse auth token"
    exit 1
fi

REG_RESP=$(curl -sk -w "\n%{http_code}" -X POST "${GC_URL}/api/satellites/register" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"satellite_name":"edge-01","selectors":["docker:label:com.docker.compose.service:satellite"],"attestation_method":"x509pop"}')
HTTP_CODE=$(echo "$REG_RESP" | tail -1)
REG_BODY=$(echo "$REG_RESP" | sed '$d')

if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Registration failed (HTTP $HTTP_CODE)"
    echo "Response: $REG_BODY"
    exit 1
fi

SAT_AGENT_ID=$(echo "$REG_BODY" | grep -o '"parent_agent_id":"[^"]*"' | cut -d'"' -f4)
echo "Satellite agent SPIFFE ID: $SAT_AGENT_ID"

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
