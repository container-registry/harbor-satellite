#!/bin/bash
# Cleanup Satellite SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Satellite (Join Token) ==="

docker compose down -v --remove-orphans

rm -f ./spire/agent-satellite-runtime.conf

echo "Cleanup complete"
