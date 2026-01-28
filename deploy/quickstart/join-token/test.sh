#!/bin/bash
# Test script for Join Token SPIFFE Authentication
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Testing Harbor Satellite SPIFFE (Join Token) ==="
echo ""

FAILED=0

# Test 1: Check SPIRE Server health
echo "[Test 1] SPIRE Server health..."
if docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock > /dev/null 2>&1; then
    echo "  PASS: SPIRE server is healthy"
else
    echo "  FAIL: SPIRE server is not healthy"
    FAILED=1
fi

# Test 2: Check SPIRE Agents attestation
echo "[Test 2] SPIRE Agent attestation..."
AGENTS=$(docker exec spire-server /opt/spire/bin/spire-server agent list -socketPath /tmp/spire-server/private/api.sock 2>&1)
if echo "$AGENTS" | grep -q "spiffe://harbor-satellite.local/agent"; then
    echo "  PASS: Agents are attested"
else
    echo "  FAIL: Agents are not attested"
    FAILED=1
fi

# Test 3: Check workload entries
echo "[Test 3] Workload entries..."
ENTRIES=$(docker exec spire-server /opt/spire/bin/spire-server entry show -socketPath /tmp/spire-server/private/api.sock 2>&1)
if echo "$ENTRIES" | grep -q "spiffe://harbor-satellite.local/ground-control"; then
    echo "  PASS: Ground Control entry exists"
else
    echo "  FAIL: Ground Control entry missing"
    FAILED=1
fi
if echo "$ENTRIES" | grep -q "spiffe://harbor-satellite.local/satellite"; then
    echo "  PASS: Satellite entry exists"
else
    echo "  FAIL: Satellite entry missing"
    FAILED=1
fi

# Test 4: Check Ground Control health
echo "[Test 4] Ground Control health..."
if curl -s http://localhost:8080/ping | grep -q "pong"; then
    echo "  PASS: Ground Control is responding"
else
    echo "  FAIL: Ground Control is not responding"
    FAILED=1
fi

# Test 5: Check SPIRE status endpoint
echo "[Test 5] Ground Control SPIRE status..."
SPIRE_STATUS=$(curl -s http://localhost:8080/spire/status 2>&1)
if echo "$SPIRE_STATUS" | grep -q "enabled"; then
    echo "  PASS: SPIFFE is enabled in Ground Control"
else
    echo "  INFO: SPIFFE status endpoint response: $SPIRE_STATUS"
fi

# Test 6: Check Satellite logs for SPIFFE connection
echo "[Test 6] Satellite SPIFFE connection..."
SATELLITE_LOGS=$(docker logs satellite 2>&1 | tail -50)
if echo "$SATELLITE_LOGS" | grep -qi "spiffe"; then
    echo "  PASS: Satellite shows SPIFFE activity"
else
    echo "  INFO: No SPIFFE activity in recent logs"
fi

# Test 7: Check Mock Harbor
echo "[Test 7] Mock Harbor registry..."
if curl -s http://localhost:5000/v2/ | grep -q "{}"; then
    echo "  PASS: Mock Harbor is accessible"
else
    echo "  FAIL: Mock Harbor is not accessible"
    FAILED=1
fi

echo ""
echo "=== Test Summary ==="
if [ $FAILED -eq 0 ]; then
    echo "All tests passed!"
    exit 0
else
    echo "Some tests failed. Check logs for details."
    echo ""
    echo "Debug commands:"
    echo "  docker compose logs spire-server"
    echo "  docker compose logs spire-agent-gc"
    echo "  docker compose logs spire-agent-satellite"
    echo "  docker compose logs ground-control"
    echo "  docker compose logs satellite"
    exit 1
fi
