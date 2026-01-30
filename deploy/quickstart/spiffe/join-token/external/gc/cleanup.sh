#!/bin/bash
# Cleanup Ground Control SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Ground Control (Join Token) ==="

docker compose down -v --remove-orphans

rm -rf ./certs
rm -f ./spire/agent-gc-runtime.conf

docker network rm harbor-satellite 2>/dev/null || true

echo "Cleanup complete"
