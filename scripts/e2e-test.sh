#!/bin/bash
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[x]${NC} $1"; }

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GC_PORT=8090
HARBOR_URL="http://localhost:8080"

cleanup() {
    log "Cleaning up..."
    [ -n "$GC_PID" ] && kill $GC_PID 2>/dev/null || true
    [ -n "$SAT_PID" ] && kill $SAT_PID 2>/dev/null || true
    rm -f "$PROJECT_ROOT/config.json" "$PROJECT_ROOT/prev_config.json" 2>/dev/null || true
    rm -rf "$PROJECT_ROOT/zot-data" 2>/dev/null || true
}
trap cleanup EXIT

log "=== E2E Test: Harbor Satellite ==="
log "Harbor URL: $HARBOR_URL"
log "Project: $PROJECT_ROOT"

# Step 1: Verify Harbor
log "Step 1: Verifying Harbor..."
if ! curl -s "$HARBOR_URL/api/v2.0/health" > /dev/null; then
    error "Harbor not reachable at $HARBOR_URL"
    exit 1
fi
log "Harbor is running"

# Step 2: Start PostgreSQL
log "Step 2: Starting PostgreSQL..."
cd "$PROJECT_ROOT/ground-control"
docker start groundcontrol-db 2>/dev/null || docker compose up -d postgres 2>/dev/null || true
sleep 3

# Wait for postgres
for i in {1..10}; do
    if docker exec groundcontrol-db pg_isready -U postgres > /dev/null 2>&1; then
        log "PostgreSQL is ready"
        break
    fi
    sleep 1
done

# Step 3: Configure and start Ground Control
log "Step 3: Starting Ground Control..."
cat > "$PROJECT_ROOT/ground-control/.env" << EOF
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=$HARBOR_URL
PORT=$GC_PORT
DB_HOST=127.0.0.1
DB_PORT=8100
DB_DATABASE=groundcontrol
DB_USERNAME=postgres
DB_PASSWORD=password
EOF

cd "$PROJECT_ROOT/ground-control"
go run main.go > /tmp/gc.log 2>&1 &
GC_PID=$!

log "Waiting for Ground Control (PID: $GC_PID)..."
for i in {1..30}; do
    if curl -s "http://localhost:$GC_PORT/health" > /dev/null 2>&1; then
        log "Ground Control is ready"
        break
    fi
    if ! kill -0 $GC_PID 2>/dev/null; then
        error "Ground Control crashed. Logs:"
        cat /tmp/gc.log
        exit 1
    fi
    sleep 1
done

# Step 4: Create test group
log "Step 4: Creating test group..."
GROUP_RESP=$(curl -s -X POST "http://localhost:$GC_PORT/groups/sync" \
    -H "Content-Type: application/json" \
    -d '{
        "group": "e2e-test-group",
        "registry": "'"$HARBOR_URL"'",
        "artifacts": [{
            "repository": "library/alpine",
            "tag": ["latest"],
            "type": "docker",
            "digest": "sha256:test123",
            "deleted": false
        }]
    }')
log "Group response: $GROUP_RESP"

# Step 5: Create config
log "Step 5: Creating satellite config..."
CONFIG_RESP=$(curl -s -X POST "http://localhost:$GC_PORT/configs" \
    -H "Content-Type: application/json" \
    -d '{
        "config_name": "e2e-test-config",
        "registry": "'"$HARBOR_URL"'",
        "config": {
            "state_config": {},
            "app_config": {
                "ground_control_url": "http://127.0.0.1:'"$GC_PORT"'",
                "log_level": "debug",
                "use_unsecure": true,
                "state_replication_interval": "@every 00h01m00s",
                "register_satellite_interval": "@every 00h01m00s",
                "local_registry": {"url": "http://0.0.0.0:8585"},
                "encrypt_config": false
            },
            "zot_config": {
                "distSpecVersion": "1.1.0",
                "storage": {"rootDirectory": "./zot-data"},
                "http": {"address": "0.0.0.0", "port": "8585"},
                "log": {"level": "info"}
            }
        }
    }')
log "Config response: $CONFIG_RESP"

# Step 6: Register satellite
log "Step 6: Registering satellite..."
SAT_RESP=$(curl -s -X POST "http://localhost:$GC_PORT/satellites" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "e2e-test-satellite",
        "groups": ["e2e-test-group"],
        "config_name": "e2e-test-config"
    }')
log "Satellite response: $SAT_RESP"

TOKEN=$(echo "$SAT_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$TOKEN" ]; then
    error "Failed to get token"
    echo "$SAT_RESP"
    exit 1
fi
log "Got token: ${TOKEN:0:30}..."

# Step 7: Run satellite
log "Step 7: Starting satellite..."
cd "$PROJECT_ROOT"

cat > config.json << EOF
{
    "state_config": {},
    "app_config": {
        "ground_control_url": "http://127.0.0.1:$GC_PORT",
        "log_level": "debug",
        "use_unsecure": true,
        "local_registry": {"url": "http://0.0.0.0:8585"}
    },
    "zot_config": {
        "distSpecVersion": "1.1.0",
        "storage": {"rootDirectory": "./zot-data"},
        "http": {"address": "0.0.0.0", "port": "8585"},
        "log": {"level": "info"}
    }
}
EOF

go run cmd/main.go --token "$TOKEN" --ground-control-url "http://127.0.0.1:$GC_PORT" --harbor-registry-url "http://127.0.0.1:8080" --json-logging=false > /tmp/sat.log 2>&1 &
SAT_PID=$!

log "Satellite started (PID: $SAT_PID)"
log "Waiting for satellite to register..."
sleep 10

# Step 8: Verify satellite registered
log "Step 8: Verifying satellite registration..."
if grep -q "Executing process" /tmp/sat.log 2>/dev/null; then
    log "Satellite is executing processes"
fi

if grep -q "Successfully registered" /tmp/sat.log 2>/dev/null || grep -q "ZTR" /tmp/sat.log 2>/dev/null; then
    log "Satellite registration working"
fi

# Show logs
log "=== Satellite Logs ==="
tail -20 /tmp/sat.log 2>/dev/null || true

log ""
log "=== E2E Test Complete ==="
log "Ground Control: http://localhost:$GC_PORT"
log "Satellite Token: ${TOKEN:0:40}..."
log ""
log "Press Ctrl+C to stop"

wait
