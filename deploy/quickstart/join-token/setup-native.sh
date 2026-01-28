#!/bin/bash
# Native setup script for Join Token SPIFFE Authentication
# Runs SPIRE server and agent directly on the host (no containers for SPIRE)
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

SPIRE_VERSION="1.10.4"
SPIRE_DIR="$SCRIPT_DIR/spire-release"

echo "=== Harbor Satellite SPIFFE Quickstart (Join Token - Native) ==="
echo ""

# Step 1: Download SPIRE
echo "[1/7] Downloading SPIRE $SPIRE_VERSION..."
if [ ! -d "$SPIRE_DIR" ]; then
    curl -s -L "https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}/spire-${SPIRE_VERSION}-linux-amd64-musl.tar.gz" | tar xz
    mv "spire-${SPIRE_VERSION}" "$SPIRE_DIR"
fi

# Create directories
mkdir -p "$SPIRE_DIR/data/server" "$SPIRE_DIR/data/agent-gc" "$SPIRE_DIR/data/agent-sat"
mkdir -p /tmp/spire-server /tmp/spire-agent-gc /tmp/spire-agent-sat

# Step 2: Create server config
echo "[2/7] Creating SPIRE server configuration..."
cat > "$SPIRE_DIR/conf/server/server.conf" << 'EOF'
server {
    bind_address = "127.0.0.1"
    bind_port = "8081"
    socket_path = "/tmp/spire-server/private/api.sock"
    trust_domain = "harbor-satellite.local"
    data_dir = "./data/server"
    log_level = "INFO"
    ca_ttl = "24h"
    default_x509_svid_ttl = "1h"
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "sqlite3"
            connection_string = "./data/server/datastore.sqlite3"
        }
    }
    NodeAttestor "join_token" {
        plugin_data {}
    }
    KeyManager "memory" {
        plugin_data {}
    }
}
EOF

# Step 3: Start SPIRE Server
echo "[3/7] Starting SPIRE server..."
mkdir -p /tmp/spire-server/private
cd "$SPIRE_DIR"
./bin/spire-server run -config conf/server/server.conf &
SPIRE_SERVER_PID=$!
echo "SPIRE Server PID: $SPIRE_SERVER_PID"
echo "$SPIRE_SERVER_PID" > "$SCRIPT_DIR/spire-server.pid"

sleep 5
echo "Checking SPIRE server health..."
./bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock

# Step 4: Generate join tokens
echo "[4/7] Generating join tokens..."
GC_TOKEN=$(./bin/spire-server token generate \
    -spiffeID spiffe://harbor-satellite.local/agent/ground-control \
    -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')
SAT_TOKEN=$(./bin/spire-server token generate \
    -spiffeID spiffe://harbor-satellite.local/agent/satellite \
    -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')

echo "Ground Control Token: $GC_TOKEN"
echo "Satellite Token: $SAT_TOKEN"

# Save tokens
echo "$GC_TOKEN" > "$SCRIPT_DIR/gc-token.txt"
echo "$SAT_TOKEN" > "$SCRIPT_DIR/sat-token.txt"

# Step 5: Create agent configs and start agents
echo "[5/7] Starting SPIRE agents..."

# Agent for Ground Control
cat > "$SPIRE_DIR/conf/agent/agent-gc.conf" << EOF
agent {
    data_dir = "./data/agent-gc"
    log_level = "INFO"
    server_address = "127.0.0.1"
    server_port = "8081"
    socket_path = "/tmp/spire-agent-gc/public/api.sock"
    trust_domain = "harbor-satellite.local"
    insecure_bootstrap = true
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }
    KeyManager "memory" {
        plugin_data {}
    }
    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
EOF

mkdir -p /tmp/spire-agent-gc/public
./bin/spire-agent run -config conf/agent/agent-gc.conf -joinToken "$GC_TOKEN" &
AGENT_GC_PID=$!
echo "Ground Control Agent PID: $AGENT_GC_PID"
echo "$AGENT_GC_PID" > "$SCRIPT_DIR/agent-gc.pid"

# Agent for Satellite
cat > "$SPIRE_DIR/conf/agent/agent-sat.conf" << EOF
agent {
    data_dir = "./data/agent-sat"
    log_level = "INFO"
    server_address = "127.0.0.1"
    server_port = "8081"
    socket_path = "/tmp/spire-agent-sat/public/api.sock"
    trust_domain = "harbor-satellite.local"
    insecure_bootstrap = true
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }
    KeyManager "memory" {
        plugin_data {}
    }
    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
EOF

mkdir -p /tmp/spire-agent-sat/public
./bin/spire-agent run -config conf/agent/agent-sat.conf -joinToken "$SAT_TOKEN" &
AGENT_SAT_PID=$!
echo "Satellite Agent PID: $AGENT_SAT_PID"
echo "$AGENT_SAT_PID" > "$SCRIPT_DIR/agent-sat.pid"

sleep 5

# Step 6: Register workloads
echo "[6/7] Registering workloads..."

# Get current user ID for unix selector
CURRENT_UID=$(id -u)

./bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/ground-control \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector unix:uid:$CURRENT_UID \
    -socketPath /tmp/spire-server/private/api.sock || true

./bin/spire-server entry create \
    -parentID spiffe://harbor-satellite.local/agent/satellite \
    -spiffeID spiffe://harbor-satellite.local/satellite \
    -selector unix:uid:$CURRENT_UID \
    -socketPath /tmp/spire-server/private/api.sock || true

echo "Registered entries:"
./bin/spire-server entry show -socketPath /tmp/spire-server/private/api.sock

# Step 7: Start Ground Control and Satellite
echo "[7/7] Starting Ground Control and Satellite..."

# Get root of project (go up from deploy/quickstart/join-token to project root)
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
echo "Project root: $PROJECT_ROOT"

# Start PostgreSQL with podman for Ground Control
echo "Starting PostgreSQL..."
podman run -d --name gc-postgres \
    -e POSTGRES_USER=harbor \
    -e POSTGRES_PASSWORD=harbor \
    -e POSTGRES_DB=harbor_satellite \
    -p 5432:5432 \
    postgres:15-alpine 2>/dev/null || true

sleep 5

# Build and run Ground Control
echo "Building Ground Control..."
cd "$PROJECT_ROOT/ground-control"
go build -o /tmp/ground-control ./main.go

echo "Starting Ground Control with SPIFFE mTLS..."
# Must run from ground-control directory for migrations
cd "$PROJECT_ROOT/ground-control"
SPIFFE_ENABLED=true \
SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent-gc/public/api.sock \
SPIFFE_TRUST_DOMAIN=harbor-satellite.local \
DB_HOST=localhost \
DB_PORT=5432 \
DB_DATABASE=harbor_satellite \
DB_USERNAME=harbor \
DB_PASSWORD=harbor \
HARBOR_URL=http://localhost:8080 \
HARBOR_USERNAME=admin \
HARBOR_PASSWORD=Harbor12345 \
PORT=9080 \
/tmp/ground-control &

GC_PID=$!
echo "Ground Control PID: $GC_PID"
echo "$GC_PID" > "$SCRIPT_DIR/ground-control.pid"

sleep 5

# Check Ground Control (HTTPS with mTLS - use curl -k for self-signed)
echo "Ground Control is running with SPIFFE mTLS on https://localhost:9080"
echo "(Note: Direct curl won't work without SPIFFE client certificate)"

# Build and run Satellite
echo "Building Satellite..."
cd "$PROJECT_ROOT"
go build -o /tmp/satellite ./cmd/main.go

echo "Starting Satellite with SPIFFE..."
SPIFFE_ENABLED=true \
SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent-sat/public/api.sock \
SPIFFE_EXPECTED_SERVER_ID=spiffe://harbor-satellite.local/ground-control \
GROUND_CONTROL_URL=https://localhost:9080 \
/tmp/satellite &

SAT_PID=$!
echo "Satellite PID: $SAT_PID"
echo "$SAT_PID" > "$SCRIPT_DIR/satellite.pid"

echo ""
echo "=== Setup Complete ==="
echo ""
echo "PIDs saved to:"
echo "  - spire-server.pid"
echo "  - agent-gc.pid"
echo "  - agent-sat.pid"
echo "  - ground-control.pid"
echo "  - satellite.pid"
echo ""
echo "To check status:"
echo "  curl http://localhost:9080/ping"
echo "  curl http://localhost:9080/spire/status"
echo ""
echo "To tear down:"
echo "  ./cleanup-native.sh"
