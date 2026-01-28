#!/bin/bash
# Setup script for Join Token SPIFFE Authentication
# This script sets up the complete Harbor Satellite environment with SPIFFE authentication
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Harbor Satellite SPIFFE Quickstart (Join Token) ==="
echo ""

# Step 1: Generate CA certificates
echo "[1/8] Generating CA certificates..."
./generate-certs.sh

# Step 2: Start infrastructure (postgres, mock-harbor, spire-server)
echo "[2/8] Starting infrastructure services..."
docker compose up -d postgres mock-harbor spire-server
echo "Waiting for SPIRE server to be healthy..."
sleep 10

# Wait for spire-server to be ready
for i in {1..30}; do
    if docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock > /dev/null 2>&1; then
        echo "SPIRE server is healthy"
        break
    fi
    echo "Waiting for SPIRE server... ($i/30)"
    sleep 2
done

# Step 3: Generate join tokens for agents
echo "[3/8] Generating join tokens for SPIRE agents..."
GC_TOKEN=$(docker exec spire-server /opt/spire/bin/spire-server token generate -spiffeID spiffe://harbor-satellite.local/agent/ground-control -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')
SATELLITE_TOKEN=$(docker exec spire-server /opt/spire/bin/spire-server token generate -spiffeID spiffe://harbor-satellite.local/agent/satellite -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')

echo "Ground Control Agent Token: $GC_TOKEN"
echo "Satellite Agent Token: $SATELLITE_TOKEN"

# Save tokens to files for agents to use
echo "$GC_TOKEN" > ./tokens/gc-token.txt
echo "$SATELLITE_TOKEN" > ./tokens/satellite-token.txt

# Step 4: Start SPIRE agents with join tokens
echo "[4/8] Starting SPIRE agents..."

# Create temporary agent configs with join tokens
mkdir -p ./tokens
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

cat > ./spire/agent-satellite-runtime.conf << EOF
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
    join_token = "$SATELLITE_TOKEN"
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

# Start agents with runtime configs
docker compose up -d spire-agent-gc spire-agent-satellite

echo "Waiting for SPIRE agents to attest..."
sleep 15

# Verify agent attestation
echo "Verifying agent attestation..."
docker exec spire-server /opt/spire/bin/spire-server agent list -socketPath /tmp/spire-server/private/api.sock

# Step 5: Register workloads
echo "[5/8] Registering SPIFFE workloads..."

# Register Ground Control workload
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/ground-control \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock || true

# Register Satellite workload
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/satellite \
    -spiffeID spiffe://harbor-satellite.local/satellite \
    -selector docker:label:com.docker.compose.service:satellite \
    -socketPath /tmp/spire-server/private/api.sock || true

echo "Listing registered entries..."
docker exec spire-server /opt/spire/bin/spire-server entry show -socketPath /tmp/spire-server/private/api.sock

# Step 6: Start Ground Control
echo "[6/8] Starting Ground Control..."
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

# Step 7: Start Satellite
echo "[7/8] Starting Satellite..."
docker compose up -d satellite

echo "Waiting for Satellite to initialize..."
sleep 10

# Step 8: Verify setup
echo "[8/8] Verifying setup..."
echo ""
echo "=== Verification ==="

echo "SPIRE Server Status:"
docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock

echo ""
echo "Registered Agents:"
docker exec spire-server /opt/spire/bin/spire-server agent list -socketPath /tmp/spire-server/private/api.sock

echo ""
echo "Registered Entries:"
docker exec spire-server /opt/spire/bin/spire-server entry show -socketPath /tmp/spire-server/private/api.sock

echo ""
echo "Ground Control SPIRE Status:"
curl -s http://localhost:8080/spire/status | jq . || echo "SPIRE status not available"

echo ""
echo "Ground Control Ping:"
curl -s http://localhost:8080/ping

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Services running:"
echo "  - PostgreSQL:      localhost:5432"
echo "  - Mock Harbor:     localhost:5000"
echo "  - SPIRE Server:    localhost:8081"
echo "  - Ground Control:  localhost:8080"
echo "  - Satellite:       (internal network)"
echo ""
echo "To view logs:"
echo "  docker compose logs -f satellite"
echo "  docker compose logs -f ground-control"
echo ""
echo "To tear down:"
echo "  ./cleanup.sh"
