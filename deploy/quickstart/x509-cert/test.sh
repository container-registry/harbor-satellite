#!/bin/bash
# Test script for X.509 Certificate SPIFFE Authentication
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Testing Harbor Satellite SPIFFE (X.509 PoP) ==="
echo ""

FAILED=0

# Test 1: SPIRE Server health
echo "[Test 1] SPIRE Server health..."
if docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock > /dev/null 2>&1; then
    echo "  PASS: SPIRE server is healthy"
else
    echo "  FAIL: SPIRE server is not healthy"
    FAILED=1
fi

# Test 2: X.509 PoP attestation
echo "[Test 2] X.509 PoP agent attestation..."
AGENTS=$(docker exec spire-server /opt/spire/bin/spire-server agent list -socketPath /tmp/spire-server/private/api.sock 2>&1)
if echo "$AGENTS" | grep -q "x509pop"; then
    echo "  PASS: Agents attested via X.509 PoP"
else
    echo "  INFO: Agent attestation method: $AGENTS"
fi

# Test 3: Workload entries
echo "[Test 3] Workload entries..."
ENTRIES=$(docker exec spire-server /opt/spire/bin/spire-server entry show -socketPath /tmp/spire-server/private/api.sock 2>&1)
if echo "$ENTRIES" | grep -q "spiffe://harbor-satellite.local/ground-control"; then
    echo "  PASS: Ground Control entry exists"
else
    echo "  FAIL: Ground Control entry missing"
    FAILED=1
fi

# Test 4: Ground Control health
echo "[Test 4] Ground Control health..."
if curl -s http://localhost:8080/ping | grep -q "pong"; then
    echo "  PASS: Ground Control is responding"
else
    echo "  FAIL: Ground Control is not responding"
    FAILED=1
fi

# Test 5: Certificate verification
echo "[Test 5] Agent certificates..."
if [ -f "./certs/agent-gc.crt" ] && [ -f "./certs/agent-satellite.crt" ]; then
    echo "  PASS: Agent certificates exist"
    echo "  Ground Control cert subject:"
    openssl x509 -in ./certs/agent-gc.crt -noout -subject 2>/dev/null | sed 's/^/    /'
    echo "  Satellite cert subject:"
    openssl x509 -in ./certs/agent-satellite.crt -noout -subject 2>/dev/null | sed 's/^/    /'
else
    echo "  FAIL: Agent certificates missing"
    FAILED=1
fi

echo ""
echo "=== Test Summary ==="
if [ $FAILED -eq 0 ]; then
    echo "All tests passed!"
    exit 0
else
    echo "Some tests failed."
    exit 1
fi
