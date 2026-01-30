#!/bin/bash
# Setup Ground Control with External SPIRE (Join Token Attestation)
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Ground Control Setup (Join Token) ==="
echo ""

# Step 1: Generate CA certificates
echo "[1/6] Generating CA certificates..."
./generate-certs.sh

# Step 2: Start postgres and SPIRE server
echo "[2/6] Starting PostgreSQL and SPIRE server..."
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

# Step 3: Generate join token for GC agent
echo "[3/6] Generating join token for GC SPIRE agent..."
GC_TOKEN=$(docker exec spire-server /opt/spire/bin/spire-server token generate \
    -spiffeID spiffe://harbor-satellite.local/agent/ground-control \
    -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')

if [ -z "$GC_TOKEN" ]; then
    echo "ERROR: Failed to generate join token"
    exit 1
fi
echo "GC Agent Token: $GC_TOKEN"

# Step 4: Create runtime agent config with token
echo "[4/6] Creating agent config with join token..."
cat > ./spire/agent-gc-runtime.conf << EOF
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
    join_token = "$GC_TOKEN"
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

# Step 5: Start GC agent and register workload
echo "[5/6] Starting SPIRE agent for Ground Control..."
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

# Register GC workload
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/ground-control \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock || true

# Step 6: Start Ground Control
echo "[6/6] Starting Ground Control..."
docker compose up -d ground-control

echo "Waiting for Ground Control to be healthy..."
for i in $(seq 1 30); do
    if curl -s http://localhost:8080/ping > /dev/null 2>&1; then
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
echo "  PostgreSQL:      localhost:5432"
echo "  SPIRE Server:    localhost:8081"
echo "  Ground Control:  localhost:8080"
echo ""
echo "Next: Set up satellite in ../sat/"
echo "Cleanup: ./cleanup.sh"
