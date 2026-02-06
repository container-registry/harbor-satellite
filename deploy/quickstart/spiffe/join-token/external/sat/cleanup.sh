#!/bin/bash
# Cleanup Satellite SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Satellite (Join Token) ==="

echo "> Deleting Satellite SPIRE entry..."
ENTRY_ID=$(docker exec spire-server /opt/spire/bin/spire-server entry show \
    -spiffeID spiffe://harbor-satellite.local/satellite/region/us-west/edge-01 \
    -socketPath /tmp/spire-server/private/api.sock 2>/dev/null \
    | grep "Entry ID" | awk '{print $4}') || true
if [ -n "$ENTRY_ID" ]; then
    docker exec spire-server /opt/spire/bin/spire-server entry delete \
        -entryID "$ENTRY_ID" \
        -socketPath /tmp/spire-server/private/api.sock 2>/dev/null || true
fi

echo "> docker compose down -v --remove-orphans"
docker compose down -v --remove-orphans

echo "> rm -f ./spire/agent-satellite-runtime.conf"
rm -f ./spire/agent-satellite-runtime.conf

# Delete robot account from Harbor
HARBOR_URL="${HARBOR_URL:-http://localhost:8080}"
HARBOR_USERNAME="${HARBOR_USERNAME:-admin}"
HARBOR_PASSWORD="${HARBOR_PASSWORD:-Harbor12345}"
SATELLITE_NAME="${SATELLITE_NAME:-edge-01}"

echo "> Querying Harbor for robot account matching ${SATELLITE_NAME}..."
ROBOT_ID=$(curl -s -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_URL}/api/v2.0/robots?q=name%3D~${SATELLITE_NAME}" \
    | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)

if [ -n "$ROBOT_ID" ]; then
    echo "> DELETE ${HARBOR_URL}/api/v2.0/robots/${ROBOT_ID}"
    curl -s -X DELETE -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
        "${HARBOR_URL}/api/v2.0/robots/${ROBOT_ID}"
    echo "Deleted robot account (ID: ${ROBOT_ID})"
else
    echo "No robot account found for ${SATELLITE_NAME}, skipping"
fi

echo "Cleanup complete"
