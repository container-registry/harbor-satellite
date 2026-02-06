#!/bin/bash
# Cleanup Ground Control X.509 PoP SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Ground Control (X.509 PoP) ==="

echo "> Deleting Ground Control SPIRE entry..."
ENTRY_ID=$(docker exec spire-server /opt/spire/bin/spire-server entry show \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -socketPath /tmp/spire-server/private/api.sock 2>/dev/null \
    | grep "Entry ID" | awk '{print $4}') || true
if [ -n "$ENTRY_ID" ]; then
    docker exec spire-server /opt/spire/bin/spire-server entry delete \
        -entryID "$ENTRY_ID" \
        -socketPath /tmp/spire-server/private/api.sock 2>/dev/null || true
fi

echo "> docker compose down -v --remove-orphans"
docker compose down -v --remove-orphans

echo "> rm -rf ./certs"
rm -rf ./certs

echo "> docker network rm harbor-satellite"
docker network rm harbor-satellite 2>/dev/null || true

echo "Cleanup complete"
