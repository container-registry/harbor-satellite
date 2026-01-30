#!/bin/bash
# Cleanup Satellite SSH PoP SPIRE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Satellite (SSH PoP) ==="

docker compose down -v --remove-orphans

echo "Cleanup complete"
