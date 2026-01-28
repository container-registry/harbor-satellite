#!/bin/bash
# Cleanup script for Join Token SPIFFE Authentication
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Harbor Satellite SPIFFE Quickstart (Join Token) ==="

# Stop and remove containers
echo "Stopping containers..."
docker compose down -v --remove-orphans

# Remove generated files
echo "Removing generated files..."
rm -rf ./certs
rm -rf ./tokens
rm -f ./spire/agent-gc-runtime.conf
rm -f ./spire/agent-satellite-runtime.conf

# Remove network if it exists
docker network rm harbor-satellite 2>/dev/null || true

echo "Cleanup complete"
