#!/bin/bash
# Setup Satellite with External SPIRE Agent (Join Token Attestation)
# Requires GC-side setup to be running (../gc/setup.sh)
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Satellite Setup (Join Token) ==="
echo ""

# Verify GC is running
echo "[1/4] Verifying Ground Control is running..."
if ! curl -sk https://localhost:${GC_HOST_PORT:-9080}/ping > /dev/null 2>&1; then
    echo "ERROR: Ground Control is not running. Run ../gc/setup.sh first."
    exit 1
fi
echo "Ground Control is reachable"

# Generate join token for satellite agent via GC API
echo "[2/4] Requesting join token from Ground Control..."

GC_URL="https://localhost:${GC_HOST_PORT:-9080}"

# Login to get Bearer token
LOGIN_RESP=$(curl -sk -w "\n%{http_code}" -X POST "${GC_URL}/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD:-Harbor12345}\"}")
HTTP_CODE=$(echo "$LOGIN_RESP" | tail -1)
LOGIN_BODY=$(echo "$LOGIN_RESP" | sed '$d')

if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Login failed (HTTP $HTTP_CODE). Check admin credentials."
    exit 1
fi

AUTH_TOKEN=$(echo "$LOGIN_BODY" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$AUTH_TOKEN" ]; then
    echo "ERROR: Failed to parse auth token from login response"
    exit 1
fi

TOKEN_RESP=$(curl -sk -X POST "${GC_URL}/api/join-tokens" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"satellite_name":"edge-01","region":"us-west"}')

SAT_TOKEN=$(echo "$TOKEN_RESP" | grep -o '"join_token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$SAT_TOKEN" ]; then
    echo "ERROR: Failed to get join token from GC"
    echo "Response: $TOKEN_RESP"
    exit 1
fi
echo "Satellite Agent Token: $SAT_TOKEN"

# Create runtime agent config with token
echo "[3/4] Creating agent config with join token..."
cat > ./spire/agent-satellite-runtime.conf << EOF
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
    join_token = "$SAT_TOKEN"
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
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

# Register satellite workload on SPIRE server (via GC's spire-server container)
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/edge-01 \
    -spiffeID spiffe://harbor-satellite.local/satellite/region/us-west/edge-01 \
    -selector docker:label:com.docker.compose.service:satellite \
    -socketPath /tmp/spire-server/private/api.sock || true

# Start satellite agent and satellite
echo "[4/4] Starting SPIRE agent and Satellite..."
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
