#!/bin/bash
# Native setup script for X.509 PoP SPIFFE Authentication
# This script runs SPIRE and components directly on the host without Docker
# (except for PostgreSQL which is needed for Ground Control)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Configuration
SPIRE_VERSION="1.10.4"
TRUST_DOMAIN="harbor-satellite.local"
HARBOR_URL="${HARBOR_URL:-http://localhost:8080}"
HARBOR_USERNAME="${HARBOR_USERNAME:-admin}"
HARBOR_PASSWORD="${HARBOR_PASSWORD:-Harbor12345}"
DB_PORT="${DB_PORT:-5433}"

echo "=== Harbor Satellite SPIFFE Quickstart (X.509 PoP - Native) ==="
echo ""
echo "This setup uses X.509 Proof of Possession (PoP) attestation."
echo "Agents authenticate to SPIRE Server using pre-provisioned certificates."
echo ""

# Step 1: Download SPIRE if not present
if [ ! -d "spire-release" ]; then
    echo "[1/8] Downloading SPIRE $SPIRE_VERSION..."
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
    esac
    curl -sL "https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}/spire-${SPIRE_VERSION}-linux-${ARCH}-musl.tar.gz" | tar xz
    mv "spire-${SPIRE_VERSION}" spire-release
else
    echo "[1/8] SPIRE already downloaded"
fi

# Step 2: Generate certificates
echo "[2/8] Generating X.509 certificates for PoP attestation..."
./generate-certs.sh

# Step 3: Create data directories
echo "[3/8] Creating data directories..."
mkdir -p spire-data/server spire-data/agent-gc spire-data/agent-sat
mkdir -p /tmp/spire-agent-gc /tmp/spire-agent-sat

# Step 4: Create native SPIRE server config
echo "[4/8] Creating native SPIRE configurations..."
cat > spire-data/server.conf << EOF
server {
    bind_address = "127.0.0.1"
    bind_port = "8081"
    socket_path = "$SCRIPT_DIR/spire-data/server/api.sock"
    trust_domain = "$TRUST_DOMAIN"
    data_dir = "$SCRIPT_DIR/spire-data/server"
    log_level = "INFO"
    ca_ttl = "24h"
    default_x509_svid_ttl = "1h"
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "sqlite3"
            connection_string = "$SCRIPT_DIR/spire-data/server/datastore.sqlite3"
        }
    }

    NodeAttestor "x509pop" {
        plugin_data {
            ca_bundle_path = "$SCRIPT_DIR/certs/x509pop-ca.crt"
        }
    }

    KeyManager "disk" {
        plugin_data {
            keys_path = "$SCRIPT_DIR/spire-data/server/keys.json"
        }
    }

    UpstreamAuthority "disk" {
        plugin_data {
            key_file_path = "$SCRIPT_DIR/certs/ca.key"
            cert_file_path = "$SCRIPT_DIR/certs/ca.crt"
        }
    }
}
EOF

# Create Ground Control agent config
cat > spire-data/agent-gc.conf << EOF
agent {
    data_dir = "$SCRIPT_DIR/spire-data/agent-gc"
    log_level = "INFO"
    server_address = "127.0.0.1"
    server_port = "8081"
    socket_path = "/tmp/spire-agent-gc/agent.sock"
    trust_bundle_path = "$SCRIPT_DIR/certs/ca.crt"
    trust_domain = "$TRUST_DOMAIN"
}

plugins {
    NodeAttestor "x509pop" {
        plugin_data {
            private_key_path = "$SCRIPT_DIR/certs/agent-gc.key"
            certificate_path = "$SCRIPT_DIR/certs/agent-gc.crt"
        }
    }

    KeyManager "disk" {
        plugin_data {
            directory = "$SCRIPT_DIR/spire-data/agent-gc"
        }
    }

    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
EOF

# Create Satellite agent config
cat > spire-data/agent-sat.conf << EOF
agent {
    data_dir = "$SCRIPT_DIR/spire-data/agent-sat"
    log_level = "INFO"
    server_address = "127.0.0.1"
    server_port = "8081"
    socket_path = "/tmp/spire-agent-sat/agent.sock"
    trust_bundle_path = "$SCRIPT_DIR/certs/ca.crt"
    trust_domain = "$TRUST_DOMAIN"
}

plugins {
    NodeAttestor "x509pop" {
        plugin_data {
            private_key_path = "$SCRIPT_DIR/certs/agent-satellite.key"
            certificate_path = "$SCRIPT_DIR/certs/agent-satellite.crt"
        }
    }

    KeyManager "disk" {
        plugin_data {
            directory = "$SCRIPT_DIR/spire-data/agent-sat"
        }
    }

    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
EOF

# Step 5: Start PostgreSQL
echo "[5/8] Starting PostgreSQL..."
docker run -d --rm --name gc-postgres \
    -e POSTGRES_USER=harbor \
    -e POSTGRES_PASSWORD=harbor \
    -e POSTGRES_DB=harbor_satellite \
    -p ${DB_PORT}:5432 \
    postgres:15-alpine

echo "Waiting for PostgreSQL to be ready..."
for i in {1..30}; do
    if docker exec gc-postgres pg_isready -U harbor -d harbor_satellite > /dev/null 2>&1; then
        echo "PostgreSQL is ready"
        break
    fi
    echo "Waiting for PostgreSQL... ($i/30)"
    sleep 2
done

# Step 6: Start SPIRE Server
echo "[6/8] Starting SPIRE Server..."
./spire-release/bin/spire-server run -config spire-data/server.conf > /tmp/spire-server.log 2>&1 &
echo $! > spire-server.pid
sleep 5

echo "Waiting for SPIRE server to be healthy..."
for i in {1..30}; do
    if ./spire-release/bin/spire-server healthcheck -socketPath spire-data/server/api.sock > /dev/null 2>&1; then
        echo "SPIRE server is healthy"
        break
    fi
    echo "Waiting for SPIRE server... ($i/30)"
    sleep 2
done

# Step 7: Start SPIRE Agents (they auto-attest with X.509 PoP)
echo "[7/8] Starting SPIRE Agents with X.509 PoP attestation..."

# Start Ground Control agent
./spire-release/bin/spire-agent run -config spire-data/agent-gc.conf > /tmp/spire-agent-gc.log 2>&1 &
echo $! > agent-gc.pid
sleep 3

# Start Satellite agent
./spire-release/bin/spire-agent run -config spire-data/agent-sat.conf > /tmp/spire-agent-sat.log 2>&1 &
echo $! > agent-sat.pid
sleep 3

# Verify agents attested
echo "Verifying agent attestation..."
./spire-release/bin/spire-server agent list -socketPath spire-data/server/api.sock

# Step 8: Register workloads
echo "[8/8] Registering SPIFFE workloads..."

# Get current user's UID for unix attestor
CURRENT_UID=$(id -u)

# Register Ground Control workload
./spire-release/bin/spire-server entry create \
    -parentID "spiffe://$TRUST_DOMAIN/agent/ground-control" \
    -spiffeID "spiffe://$TRUST_DOMAIN/ground-control" \
    -selector "unix:uid:$CURRENT_UID" \
    -socketPath spire-data/server/api.sock || true

# Register Satellite workload (with path selector)
./spire-release/bin/spire-server entry create \
    -parentID "spiffe://$TRUST_DOMAIN/agent/satellite" \
    -spiffeID "spiffe://$TRUST_DOMAIN/satellite" \
    -selector "unix:uid:$CURRENT_UID" \
    -socketPath spire-data/server/api.sock || true

echo ""
echo "=== X.509 PoP Setup Complete ==="
echo ""
echo "SPIRE is now running with X.509 PoP attestation."
echo "The agents authenticated using pre-provisioned certificates."
echo ""
echo "To verify agents:"
echo "  ./spire-release/bin/spire-server agent list -socketPath spire-data/server/api.sock"
echo ""
echo "To start Ground Control:"
echo "  export SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent-gc/agent.sock"
echo "  export SPIFFE_ENABLED=true"
echo "  export SPIFFE_TRUST_DOMAIN=$TRUST_DOMAIN"
echo "  cd ../../../../ground-control && go run main.go"
echo ""
echo "To start Satellite:"
echo "  export SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent-sat/agent.sock"
echo "  export SPIFFE_ENABLED=true"
echo "  export GROUND_CONTROL_URL=https://localhost:9080"
echo "  cd ../../../.. && go run cmd/main.go"
echo ""
echo "Logs:"
echo "  SPIRE Server: /tmp/spire-server.log"
echo "  Agent GC:     /tmp/spire-agent-gc.log"
echo "  Agent Sat:    /tmp/spire-agent-sat.log"
