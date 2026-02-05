#!/bin/bash
# Cleanup Ground Control X.509 PoP SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Ground Control (X.509 PoP) ==="

echo "> docker compose down -v --remove-orphans"
docker compose down -v --remove-orphans

echo "> rm -rf ./certs"
rm -rf ./certs

echo "> docker network rm harbor-satellite"
docker network rm harbor-satellite 2>/dev/null || true

echo "Cleanup complete"
