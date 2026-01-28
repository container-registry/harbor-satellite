#!/bin/bash
# Cleanup script for X.509 PoP native SPIFFE setup

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Native X.509 PoP SPIFFE setup ==="

# Kill processes from PID files
for pidfile in satellite.pid ground-control.pid agent-sat.pid agent-gc.pid spire-server.pid; do
    if [ -f "$pidfile" ]; then
        pid=$(cat "$pidfile")
        echo "Killing $pidfile (PID: $pid)..."
        kill "$pid" 2>/dev/null || true
        rm -f "$pidfile"
    fi
done

# Stop PostgreSQL
echo "Stopping PostgreSQL..."
docker stop gc-postgres 2>/dev/null || true
docker rm gc-postgres 2>/dev/null || true

# Clean up socket directories
rm -rf /tmp/spire-agent-gc /tmp/spire-agent-sat

# Optionally clean data directories (uncomment to fully reset)
# rm -rf spire-data

echo "Cleanup complete"
