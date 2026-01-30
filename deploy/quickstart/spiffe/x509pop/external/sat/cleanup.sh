#!/bin/bash
# Cleanup Satellite X.509 PoP SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Satellite (X.509 PoP) ==="

docker compose down -v --remove-orphans

echo "Cleanup complete"
