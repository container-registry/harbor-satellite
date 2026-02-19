#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
GC_DIR="$PROJECT_ROOT/ground-control"

# Default Harbor settings - update these for your environment
HARBOR_URL="${HARBOR_URL:-http://localhost:8080}"
HARBOR_USERNAME="${HARBOR_USERNAME:-admin}"
HARBOR_PASSWORD="${HARBOR_PASSWORD:-Harbor12345}"

# Ground Control settings
GC_PORT=8090
DB_PORT=5432
DB_HOST=127.0.0.1
DB_NAME=groundcontrol
DB_USER=postgres
DB_PASS=password

cleanup() {
    log_info "Cleaning up..."
    # Kill background processes
    if [ -n "$GC_PID" ]; then
        kill $GC_PID 2>/dev/null || true
    fi
    if [ -n "$SAT_PID" ]; then
        kill $SAT_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Step 1: Check PostgreSQL
check_postgres() {
    log_info "Checking PostgreSQL..."

    if ! command -v psql &> /dev/null; then
        log_warn "psql not found, checking if postgres container is running..."
    fi

    # Check if postgres container is running
    if docker ps --format '{{.Names}}' | grep -q groundcontrol-db; then
        log_info "PostgreSQL container is already running"
        DB_HOST=127.0.0.1
        DB_PORT=8100  # Mapped port from docker-compose
    else
        log_info "Starting PostgreSQL container..."
        cd "$GC_DIR"
        docker compose up -d postgres
        sleep 5
        DB_PORT=8100
    fi
}

# Step 2: Setup Ground Control .env
setup_gc_env() {
    log_info "Setting up Ground Control environment..."

    cat > "$GC_DIR/.env" << EOF
HARBOR_USERNAME=$HARBOR_USERNAME
HARBOR_PASSWORD=$HARBOR_PASSWORD
HARBOR_URL=$HARBOR_URL

PORT=$GC_PORT

DB_HOST=$DB_HOST
DB_PORT=$DB_PORT
DB_DATABASE=$DB_NAME
DB_USERNAME=$DB_USER
DB_PASSWORD=$DB_PASS
EOF

    log_info "Created $GC_DIR/.env"
}

# Step 3: Start Ground Control
start_ground_control() {
    log_info "Starting Ground Control on port $GC_PORT..."

    cd "$GC_DIR"
    go run main.go &
    GC_PID=$!

    # Wait for Ground Control to be ready
    log_info "Waiting for Ground Control to start..."
    for i in {1..30}; do
        if curl -s "http://localhost:$GC_PORT/health" > /dev/null 2>&1; then
            log_info "Ground Control is ready!"
            return 0
        fi
        sleep 1
    done

    log_error "Ground Control failed to start"
    return 1
}

# Step 4: Create test group
create_test_group() {
    log_info "Creating test group..."

    response=$(curl -s -X POST "http://localhost:$GC_PORT/groups/sync" \
        -H "Content-Type: application/json" \
        -d '{
            "group": "test-group",
            "registry": "'"$HARBOR_URL"'",
            "artifacts": [
                {
                    "repository": "library/alpine",
                    "tag": ["latest"],
                    "type": "docker",
                    "digest": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
                    "deleted": false
                }
            ]
        }')

    log_info "Group creation response: $response"
}

# Step 5: Create config
create_test_config() {
    log_info "Creating test config..."

    response=$(curl -s -X POST "http://localhost:$GC_PORT/configs" \
        -H "Content-Type: application/json" \
        -d '{
            "config_name": "test-config",
            "registry": "'"$HARBOR_URL"'",
            "config": {
                "state_config": {},
                "app_config": {
                    "ground_control_url": "http://127.0.0.1:'"$GC_PORT"'",
                    "log_level": "debug",
                    "use_unsecure": true,
                    "state_replication_interval": "@every 00h00m30s",
                    "register_satellite_interval": "@every 00h00m30s",
                    "local_registry": {
                        "url": "http://0.0.0.0:8585"
                    },
                    "encrypt_config": false
                },
                "zot_config": {
                    "distSpecVersion": "1.1.0",
                    "storage": {
                        "rootDirectory": "./zot-data"
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
        }')

    log_info "Config creation response: $response"
}

# Step 6: Register satellite
register_satellite() {
    log_info "Registering satellite..."

    response=$(curl -s -X POST "http://localhost:$GC_PORT/satellites" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "test-satellite",
            "groups": ["test-group"],
            "config_name": "test-config"
        }')

    log_info "Satellite registration response: $response"

    # Extract token from response
    TOKEN=$(echo "$response" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$TOKEN" ]; then
        log_error "Failed to get token from registration"
        echo "Response: $response"
        return 1
    fi

    log_info "Got satellite token: ${TOKEN:0:20}..."
    echo "$TOKEN"
}

# Step 7: Run satellite
run_satellite() {
    local token="$1"

    log_info "Starting satellite..."

    cd "$PROJECT_ROOT"

    # Create a minimal config file
    cat > config.json << EOF
{
    "state_config": {},
    "app_config": {
        "ground_control_url": "http://127.0.0.1:$GC_PORT",
        "log_level": "debug",
        "use_unsecure": true,
        "state_replication_interval": "@every 00h00m30s",
        "register_satellite_interval": "@every 00h00m30s",
        "local_registry": {
            "url": "http://0.0.0.0:8585"
        }
    },
    "zot_config": {
        "distSpecVersion": "1.1.0",
        "storage": {
            "rootDirectory": "./zot-data"
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

    go run cmd/main.go --token "$token" --ground-control-url "http://127.0.0.1:$GC_PORT" --harbor-registry-url "http://127.0.0.1:8080" --json-logging=false &
    SAT_PID=$!

    log_info "Satellite started with PID $SAT_PID"
}

# Main
main() {
    log_info "Starting local test setup..."
    log_info "Harbor URL: $HARBOR_URL"
    log_info "Project root: $PROJECT_ROOT"

    check_postgres
    setup_gc_env
    start_ground_control

    sleep 2

    create_test_group
    create_test_config

    TOKEN=$(register_satellite)

    if [ -n "$TOKEN" ]; then
        run_satellite "$TOKEN"

        log_info ""
        log_info "=========================================="
        log_info "Setup complete!"
        log_info "=========================================="
        log_info "Ground Control: http://localhost:$GC_PORT"
        log_info "Satellite Token: ${TOKEN:0:30}..."
        log_info ""
        log_info "Press Ctrl+C to stop all services"

        # Wait for user to stop
        wait
    else
        log_error "Failed to register satellite"
        exit 1
    fi
}

main "$@"
