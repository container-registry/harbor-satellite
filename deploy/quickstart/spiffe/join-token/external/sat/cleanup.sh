#!/bin/bash
# Cleanup Satellite SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Satellite (Join Token) ==="

docker compose down -v --remove-orphans

rm -f ./spire/agent-satellite-runtime.conf

# Delete robot account from Harbor
HARBOR_URL="${HARBOR_URL:-http://localhost:8080}"
HARBOR_USERNAME="${HARBOR_USERNAME:-admin}"
HARBOR_PASSWORD="${HARBOR_PASSWORD:-Harbor12345}"
SATELLITE_NAME="${SATELLITE_NAME:-edge-01}"

echo "Deleting robot account for satellite ${SATELLITE_NAME} from Harbor..."
ROBOT_ID=$(curl -s -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_URL}/api/v2.0/robots?q=name%3D~${SATELLITE_NAME}" \
    | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)

if [ -n "$ROBOT_ID" ]; then
    curl -s -X DELETE -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
        "${HARBOR_URL}/api/v2.0/robots/${ROBOT_ID}"
    echo "Deleted robot account (ID: ${ROBOT_ID})"
else
    echo "No robot account found for ${SATELLITE_NAME}, skipping"
fi

echo "Cleanup complete"
