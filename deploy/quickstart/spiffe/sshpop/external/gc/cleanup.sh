#!/bin/bash
# Cleanup Ground Control SSH PoP SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Ground Control (SSH PoP) ==="

docker compose down -v --remove-orphans

rm -rf ./certs

docker network rm harbor-satellite 2>/dev/null || true

echo "Cleanup complete"
