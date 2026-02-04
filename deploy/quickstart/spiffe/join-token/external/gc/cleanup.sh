#!/bin/bash
# Cleanup Ground Control SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Ground Control (Join Token) ==="

echo "> docker compose down -v --remove-orphans"
docker compose down -v --remove-orphans

echo "> rm -rf ./certs"
rm -rf ./certs

echo "> rm -f ./spire/agent-gc-runtime.conf"
rm -f ./spire/agent-gc-runtime.conf

echo "> docker network rm harbor-satellite"
docker network rm harbor-satellite 2>/dev/null || true

echo "Cleanup complete"
