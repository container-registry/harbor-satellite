#!/bin/bash
# Cleanup script for Native SPIFFE setup
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Cleaning up Native SPIFFE setup ==="

# Kill processes
for pidfile in satellite.pid ground-control.pid agent-sat.pid agent-gc.pid spire-server.pid; do
    if [ -f "$pidfile" ]; then
        pid=$(cat "$pidfile")
        echo "Killing $pidfile (PID: $pid)..."
        kill "$pid" 2>/dev/null || true
        rm -f "$pidfile"
    fi
done

# Stop PostgreSQL container
echo "Stopping PostgreSQL..."
podman stop gc-postgres 2>/dev/null || true
podman rm gc-postgres 2>/dev/null || true

# Clean up temp directories
rm -rf /tmp/spire-server /tmp/spire-agent-gc /tmp/spire-agent-sat
rm -f /tmp/ground-control /tmp/satellite

# Clean up tokens
rm -f gc-token.txt sat-token.txt

# Clean up SPIRE data
rm -rf spire-release/data 2>/dev/null || true

echo "Cleanup complete"
