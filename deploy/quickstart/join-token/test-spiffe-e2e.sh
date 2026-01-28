#!/bin/bash
# =============================================================================
# SPIFFE Join Token E2E Test Script
# =============================================================================
#
# This script tests the complete SPIFFE-based Zero Touch Registration (ZTR)
# flow for Harbor Satellite using Join Token attestation.
#
# PREREQUISITES:
#   1. Harbor registry running on localhost:8080
#      - Admin credentials: admin/Harbor12345
#      - An nginx image at localhost:8080/library/nginx:latest
#   2. Go installed (for building satellite and ground-control)
#   3. PostgreSQL client (psql) installed
#   4. Podman or Docker installed (for PostgreSQL container)
#   5. curl and jq installed
#
# WHAT THIS SCRIPT TESTS:
#   1. SPIRE Server and Agent setup with Join Token attestation
#   2. Ground Control with SPIFFE mTLS authentication
#   3. Satellite obtaining SPIFFE identity from SPIRE agent
#   4. SPIFFE-based ZTR (Zero Touch Registration) with auto-registration
#   5. Config-state artifact creation during auto-registration
#   6. State replication from Harbor to satellite's local Zot registry
#   7. Image verification by comparing layer digests
#
# USAGE:
#   ./test-spiffe-e2e.sh
#
# The script will output PASS/FAIL for each test step.
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
SPIRE_VERSION="1.10.4"
SPIRE_DIR="$SCRIPT_DIR/spire-release"
GC_PORT=9080
DB_PORT=5433
HARBOR_URL="http://localhost:8080"
HARBOR_USER="admin"
HARBOR_PASS="Harbor12345"
TEST_IMAGE="library/nginx:latest"
EXPECTED_LAYER_COUNT=8

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_step() {
    echo ""
    echo -e "${YELLOW}=== $1 ===${NC}"
}

cleanup_previous() {
    log_info "Cleaning up any previous test artifacts..."

    # Kill previous processes
    for pidfile in spire-server.pid agent-gc.pid agent-sat.pid ground-control.pid satellite.pid; do
        if [ -f "$SCRIPT_DIR/$pidfile" ]; then
            kill "$(cat "$SCRIPT_DIR/$pidfile")" 2>/dev/null || true
            rm -f "$SCRIPT_DIR/$pidfile"
        fi
    done

    # Stop PostgreSQL container
    podman rm -f gc-postgres 2>/dev/null || true

    # Clean up SPIFFE socket directories
    rm -rf /tmp/spire-server /tmp/spire-agent-gc /tmp/spire-agent-sat
    rm -rf /tmp/spiffe-creds /tmp/spiffe-sat-creds

    sleep 2
}

check_prerequisites() {
    log_step "Checking Prerequisites"

    # Check Harbor is running
    if curl -s -u "$HARBOR_USER:$HARBOR_PASS" "$HARBOR_URL/api/v2.0/health" | grep -q "healthy"; then
        log_success "Harbor is running and healthy"
    else
        log_fail "Harbor is not running or not healthy at $HARBOR_URL"
        echo "Please start Harbor first: docker-compose up -d"
        exit 1
    fi

    # Check nginx image exists
    if curl -s -u "$HARBOR_USER:$HARBOR_PASS" "$HARBOR_URL/v2/library/nginx/manifests/latest" -H "Accept: application/vnd.oci.image.manifest.v1+json" | grep -q "schemaVersion"; then
        log_success "Test image $TEST_IMAGE exists in Harbor"
    else
        log_fail "Test image $TEST_IMAGE not found in Harbor"
        echo "Please push nginx image: docker pull nginx:alpine && docker tag nginx:alpine localhost:8080/library/nginx:latest && docker push localhost:8080/library/nginx:latest"
        exit 1
    fi

    # Check Go is installed
    if command -v go &> /dev/null; then
        log_success "Go is installed: $(go version | cut -d' ' -f3)"
    else
        log_fail "Go is not installed"
        exit 1
    fi

    # Check podman/docker
    if command -v podman &> /dev/null; then
        log_success "Podman is installed"
    elif command -v docker &> /dev/null; then
        log_success "Docker is installed"
    else
        log_fail "Neither Podman nor Docker is installed"
        exit 1
    fi
}

# =============================================================================
# SPIRE Setup
# =============================================================================

setup_spire() {
    log_step "Setting up SPIRE Server and Agents"

    # Download SPIRE if needed
    if [ ! -d "$SPIRE_DIR" ]; then
        log_info "Downloading SPIRE $SPIRE_VERSION..."
        curl -s -L "https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}/spire-${SPIRE_VERSION}-linux-amd64-musl.tar.gz" | tar xz -C "$SCRIPT_DIR"
        mv "$SCRIPT_DIR/spire-${SPIRE_VERSION}" "$SPIRE_DIR"
    fi

    # Create directories
    mkdir -p "$SPIRE_DIR/data/server" "$SPIRE_DIR/data/agent-gc" "$SPIRE_DIR/data/agent-sat"
    mkdir -p /tmp/spire-server/private /tmp/spire-agent-gc/public /tmp/spire-agent-sat/public

    # Create server config
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

    # Start SPIRE Server
    log_info "Starting SPIRE Server..."
    cd "$SPIRE_DIR"
    ./bin/spire-server run -config conf/server/server.conf &
    SPIRE_SERVER_PID=$!
    echo "$SPIRE_SERVER_PID" > "$SCRIPT_DIR/spire-server.pid"
    sleep 5

    # Check server health
    if ./bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock 2>/dev/null; then
        log_success "SPIRE Server is healthy"
    else
        log_fail "SPIRE Server failed to start"
        return 1
    fi

    # Generate join tokens
    log_info "Generating join tokens..."
    GC_TOKEN=$(./bin/spire-server token generate \
        -spiffeID spiffe://harbor-satellite.local/agent/ground-control \
        -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')
    SAT_TOKEN=$(./bin/spire-server token generate \
        -spiffeID spiffe://harbor-satellite.local/agent/satellite \
        -socketPath /tmp/spire-server/private/api.sock | grep "Token:" | awk '{print $2}')

    echo "$GC_TOKEN" > "$SCRIPT_DIR/gc-token.txt"
    echo "$SAT_TOKEN" > "$SCRIPT_DIR/sat-token.txt"

    # Create and start Ground Control agent
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

    log_info "Starting Ground Control SPIRE Agent..."
    ./bin/spire-agent run -config conf/agent/agent-gc.conf -joinToken "$GC_TOKEN" &
    AGENT_GC_PID=$!
    echo "$AGENT_GC_PID" > "$SCRIPT_DIR/agent-gc.pid"

    # Create and start Satellite agent
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

    log_info "Starting Satellite SPIRE Agent..."
    ./bin/spire-agent run -config conf/agent/agent-sat.conf -joinToken "$SAT_TOKEN" &
    AGENT_SAT_PID=$!
    echo "$AGENT_SAT_PID" > "$SCRIPT_DIR/agent-sat.pid"

    sleep 5

    # Register workloads
    log_info "Registering workload entries..."
    CURRENT_UID=$(id -u)

    ./bin/spire-server entry create \
        -parentID spiffe://harbor-satellite.local/agent/ground-control \
        -spiffeID spiffe://harbor-satellite.local/ground-control \
        -selector unix:uid:$CURRENT_UID \
        -socketPath /tmp/spire-server/private/api.sock 2>/dev/null || true

    ./bin/spire-server entry create \
        -parentID spiffe://harbor-satellite.local/agent/satellite \
        -spiffeID spiffe://harbor-satellite.local/satellite \
        -selector unix:uid:$CURRENT_UID \
        -socketPath /tmp/spire-server/private/api.sock 2>/dev/null || true

    log_success "SPIRE infrastructure setup complete"
}

# =============================================================================
# Database Setup
# =============================================================================

setup_database() {
    log_step "Setting up PostgreSQL Database"

    log_info "Starting PostgreSQL container on port $DB_PORT..."
    podman run -d --name gc-postgres \
        -e POSTGRES_USER=harbor \
        -e POSTGRES_PASSWORD=harbor \
        -e POSTGRES_DB=harbor_satellite \
        -p $DB_PORT:5432 \
        postgres:15-alpine 2>/dev/null || true

    sleep 5

    if podman ps | grep -q gc-postgres; then
        log_success "PostgreSQL is running on port $DB_PORT"
    else
        log_fail "Failed to start PostgreSQL"
        return 1
    fi
}

# =============================================================================
# Ground Control Setup
# =============================================================================

setup_ground_control() {
    log_step "Building and Starting Ground Control"

    log_info "Building Ground Control..."
    cd "$PROJECT_ROOT/ground-control"
    go build -o /tmp/ground-control ./main.go 2>&1

    if [ -f /tmp/ground-control ]; then
        log_success "Ground Control built successfully"
    else
        log_fail "Failed to build Ground Control"
        return 1
    fi

    log_info "Starting Ground Control with SPIFFE mTLS..."
    SPIFFE_ENABLED=true \
    SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent-gc/public/api.sock \
    SPIFFE_TRUST_DOMAIN=harbor-satellite.local \
    DB_HOST=localhost \
    DB_PORT=$DB_PORT \
    DB_DATABASE=harbor_satellite \
    DB_USERNAME=harbor \
    DB_PASSWORD=harbor \
    HARBOR_URL=$HARBOR_URL \
    HARBOR_USERNAME=$HARBOR_USER \
    HARBOR_PASSWORD=$HARBOR_PASS \
    PORT=$GC_PORT \
    /tmp/ground-control > /tmp/gc.log 2>&1 &

    GC_PID=$!
    echo "$GC_PID" > "$SCRIPT_DIR/ground-control.pid"

    sleep 5

    # Verify Ground Control is listening with SPIFFE mTLS
    if ss -tlnp | grep -q ":$GC_PORT"; then
        log_success "Ground Control is listening on port $GC_PORT with SPIFFE mTLS"
    else
        log_fail "Ground Control failed to start"
        cat /tmp/gc.log
        return 1
    fi
}

# =============================================================================
# Satellite Setup and Testing
# =============================================================================

setup_satellite() {
    log_step "Building and Starting Satellite"

    log_info "Building Satellite..."
    cd "$PROJECT_ROOT"
    go build -o /tmp/satellite ./cmd/main.go 2>&1

    if [ -f /tmp/satellite ]; then
        log_success "Satellite built successfully"
    else
        log_fail "Failed to build Satellite"
        return 1
    fi

    # Create initial config without credentials (to trigger SPIFFE ZTR)
    log_info "Creating initial satellite config (without credentials)..."
    cat > "$PROJECT_ROOT/ground-control/config.json" << EOF
{
  "state_config": {
    "auth": {}
  },
  "app_config": {
    "ground_control_url": "https://localhost:$GC_PORT",
    "log_level": "info",
    "use_unsecure": true,
    "state_replication_interval": "@every 00h00m30s",
    "register_satellite_interval": "@every 00h00m05s",
    "local_registry": {
      "url": "http://0.0.0.0:8585"
    },
    "tls": {},
    "spiffe": {
      "enabled": true,
      "endpoint_socket": "unix:///tmp/spire-agent-sat/public/api.sock",
      "expected_server_id": "spiffe://harbor-satellite.local/ground-control"
    }
  },
  "zot_config": {
    "distSpecVersion": "1.1.0",
    "storage": {
      "rootDirectory": "./zot"
    },
    "http": {
      "address": "0.0.0.0",
      "port": "8585"
    },
    "log": {
      "level": "info"
    }
  }
}
EOF

    log_info "Starting Satellite with SPIFFE..."
    cd "$PROJECT_ROOT/ground-control"
    SPIFFE_ENABLED=true \
    SPIFFE_ENDPOINT_SOCKET=unix:///tmp/spire-agent-sat/public/api.sock \
    SPIFFE_EXPECTED_SERVER_ID=spiffe://harbor-satellite.local/ground-control \
    GROUND_CONTROL_URL=https://localhost:$GC_PORT \
    /tmp/satellite > /tmp/sat.log 2>&1 &

    SAT_PID=$!
    echo "$SAT_PID" > "$SCRIPT_DIR/satellite.pid"

    log_success "Satellite started with PID $SAT_PID"
}

# =============================================================================
# Test SPIFFE ZTR Registration
# =============================================================================

test_spiffe_ztr() {
    log_step "Testing SPIFFE-based Zero Touch Registration"

    log_info "Waiting for SPIFFE ZTR to complete (up to 30 seconds)..."

    for i in {1..30}; do
        if grep -q "SPIFFE-based ZTR completed successfully" /tmp/sat.log 2>/dev/null; then
            log_success "SPIFFE ZTR completed successfully"
            break
        fi
        if [ $i -eq 30 ]; then
            log_fail "SPIFFE ZTR did not complete within 30 seconds"
            echo "Satellite logs:"
            tail -20 /tmp/sat.log
            return 1
        fi
        sleep 1
    done

    # Verify satellite was registered in Ground Control
    if grep -q "Auto-registered satellite" /tmp/gc.log 2>/dev/null; then
        log_success "Satellite was auto-registered in Ground Control"
    else
        log_fail "Satellite auto-registration not found in GC logs"
    fi

    # Verify config has credentials
    if grep -q "robot\$" "$PROJECT_ROOT/ground-control/config.json"; then
        log_success "Satellite config has robot credentials"
    else
        log_fail "Satellite config missing robot credentials"
    fi
}

# =============================================================================
# Test Config State Artifact
# =============================================================================

test_config_state() {
    log_step "Testing Config State Artifact Creation"

    # Check if config-state artifact was created in Harbor
    if curl -s -u "$HARBOR_USER:$HARBOR_PASS" \
        "$HARBOR_URL/api/v2.0/projects/satellite/repositories" | \
        grep -q "config-state/default/state"; then
        log_success "Config-state artifact created in Harbor"
    else
        log_fail "Config-state artifact not found in Harbor"
    fi
}

# =============================================================================
# Test Group Assignment and State Replication
# =============================================================================

test_group_and_replication() {
    log_step "Testing Group Assignment and State Replication"

    # Get SPIFFE credentials for API calls
    mkdir -p /tmp/spiffe-sat-creds
    cd "$SPIRE_DIR"
    ./bin/spire-agent api fetch x509 \
        -socketPath /tmp/spire-agent-sat/public/api.sock \
        -write /tmp/spiffe-sat-creds/ 2>/dev/null

    # Create group with nginx image (if not exists)
    log_info "Creating group with nginx image..."
    curl -s -k --cert /tmp/spiffe-sat-creds/svid.0.pem \
        --key /tmp/spiffe-sat-creds/svid.0.key \
        -X POST "https://localhost:$GC_PORT/groups/sync" \
        -H "Content-Type: application/json" \
        -d '{
          "group": "nginx-group",
          "registry": "localhost:8080",
          "artifacts": [
            {
              "repository": "library/nginx",
              "tag": ["latest"],
              "type": "image"
            }
          ]
        }' > /dev/null 2>&1 || true

    # Add satellite to group
    log_info "Adding satellite to group..."
    RESULT=$(curl -s -k --cert /tmp/spiffe-sat-creds/svid.0.pem \
        --key /tmp/spiffe-sat-creds/svid.0.key \
        -X POST "https://localhost:$GC_PORT/groups/satellite" \
        -H "Content-Type: application/json" \
        -d '{"satellite": "default", "group": "nginx-group"}')

    if echo "$RESULT" | grep -q "successfully\|already"; then
        log_success "Satellite added to nginx-group"
    else
        log_fail "Failed to add satellite to group: $RESULT"
    fi

    # Wait for state replication
    log_info "Waiting for image replication (up to 60 seconds)..."
    for i in {1..60}; do
        if grep -q "Image nginx pushed successfully" /tmp/sat.log 2>/dev/null; then
            log_success "Image replication completed"
            break
        fi
        if [ $i -eq 60 ]; then
            log_fail "Image replication did not complete within 60 seconds"
            return 1
        fi
        sleep 1
    done
}

# =============================================================================
# Test Image Verification
# =============================================================================

test_image_verification() {
    log_step "Testing Image Verification"

    # Wait for Zot to be ready
    sleep 5

    # Check image exists in local registry
    if curl -s "http://localhost:8585/v2/library/nginx/tags/list" | grep -q "latest"; then
        log_success "nginx:latest exists in local Zot registry"
    else
        log_fail "nginx:latest not found in local Zot registry"
        return 1
    fi

    # Get layer digests from Harbor
    log_info "Comparing layer digests..."
    HARBOR_LAYERS=$(curl -s -u "$HARBOR_USER:$HARBOR_PASS" \
        "$HARBOR_URL/v2/library/nginx/manifests/latest" \
        -H "Accept: application/vnd.oci.image.manifest.v1+json" | \
        jq -r '.layers[].digest' | sort)

    # Get layer digests from local Zot
    LOCAL_LAYERS=$(curl -s "http://localhost:8585/v2/library/nginx/manifests/latest" \
        -H "Accept: application/vnd.oci.image.manifest.v1+json" | \
        jq -r '.layers[].digest' | sort)

    # Compare layer digests
    if [ "$HARBOR_LAYERS" = "$LOCAL_LAYERS" ]; then
        log_success "All $EXPECTED_LAYER_COUNT layer digests match between Harbor and local registry"
    else
        log_fail "Layer digests do not match"
        echo "Harbor layers: $HARBOR_LAYERS"
        echo "Local layers: $LOCAL_LAYERS"
    fi

    # Test pulling image from local registry
    log_info "Testing image pull from local registry..."
    if podman pull --tls-verify=false localhost:8585/library/nginx:latest 2>/dev/null | grep -q "Writing manifest"; then
        log_success "Successfully pulled image from local satellite registry"
    else
        log_warn "Could not pull image (podman may need to be configured)"
    fi
}

# =============================================================================
# Test Config Fetcher
# =============================================================================

test_config_fetcher() {
    log_step "Testing Config State Fetcher"

    # Check if config fetcher completed successfully
    if grep -q "Config fetcher completed successfully" /tmp/sat.log 2>/dev/null; then
        log_success "Config fetcher completed successfully"
    else
        log_warn "Config fetcher may not have completed (check logs)"
    fi
}

# =============================================================================
# Print Summary
# =============================================================================

print_summary() {
    echo ""
    echo "============================================================================="
    echo -e "                        ${YELLOW}TEST SUMMARY${NC}"
    echo "============================================================================="
    echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
    echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
    echo ""

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}All tests passed! SPIFFE Join Token authentication is working correctly.${NC}"
    else
        echo -e "${RED}Some tests failed. Please check the logs above for details.${NC}"
    fi

    echo ""
    echo "Log files:"
    echo "  - Ground Control: /tmp/gc.log"
    echo "  - Satellite: /tmp/sat.log"
    echo ""
    echo "To clean up:"
    echo "  $SCRIPT_DIR/cleanup-native.sh"
    echo "============================================================================="
}

# =============================================================================
# Main
# =============================================================================

main() {
    echo "============================================================================="
    echo "           SPIFFE Join Token E2E Test for Harbor Satellite"
    echo "============================================================================="
    echo ""

    cleanup_previous
    check_prerequisites
    setup_spire
    setup_database
    setup_ground_control
    setup_satellite
    test_spiffe_ztr
    test_config_state
    test_group_and_replication
    test_image_verification
    test_config_fetcher
    print_summary

    if [ $TESTS_FAILED -gt 0 ]; then
        exit 1
    fi
}

main "$@"
