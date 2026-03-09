#!/bin/bash
# pre-demo-var-check.sh
# Validates all prerequisites and variables before running the demo
set -euo pipefail

# ─── Configuration ───────────────────────────────────────────────────────────
CLOUD_IP="${CLOUD_IP:-10.147.106.55}"
HARBOR_URL="${HARBOR_URL:-http://${CLOUD_IP}:8080}"
HARBOR_USERNAME="${HARBOR_USERNAME:-admin}"
HARBOR_PASSWORD="${HARBOR_PASSWORD:-Harbor12345}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-Harbor12345}"
SAT_USER="${SAT_USER:-sat-1}"
SAT_IP="${SAT_IP:-10.147.106.144}"
SAT_UID="${SAT_UID:-1000}"
SAT_NAME="${SAT_NAME:-us-east-1}"
DEMO_IMAGE="${DEMO_IMAGE:-library/nginx}"
DEMO_TAG="${DEMO_TAG:-latest}"
GC_HOST_PORT="${GC_HOST_PORT:-9080}"
SPIRE_HOST_PORT="${SPIRE_HOST_PORT:-9081}"

PASS=0
FAIL=0
WARN=0

pass()  { PASS=$((PASS + 1)); echo "  [PASS] $1"; }
fail()  { FAIL=$((FAIL + 1)); echo "  [FAIL] $1"; }
warn()  { WARN=$((WARN + 1)); echo "  [WARN] $1"; }

echo "============================================="
echo "  Harbor Satellite Demo - Pre-flight Check"
echo "============================================="
echo ""
echo "Configuration:"
echo "  Cloud IP          : $CLOUD_IP"
echo "  Harbor URL        : $HARBOR_URL"
echo "  Harbor Username   : $HARBOR_USERNAME"
echo "  Harbor Password   : ${HARBOR_PASSWORD:0:4}****"
echo "  Admin Password    : ${ADMIN_PASSWORD:0:4}****"
echo "  Satellite User    : $SAT_USER"
echo "  Satellite IP      : $SAT_IP"
echo "  Satellite UID     : $SAT_UID"
echo "  Satellite Name    : $SAT_NAME"
echo "  Demo Image        : $DEMO_IMAGE:$DEMO_TAG"
echo "  GC Host Port      : $GC_HOST_PORT"
echo "  SPIRE Host Port   : $SPIRE_HOST_PORT"
echo ""

# ─── Cloud-side tool checks ──────────────────────────────────────────────────
echo "--- Cloud-side tools ---"

for cmd in docker curl jq openssl ssh scp; do
    if command -v "$cmd" &>/dev/null; then
        pass "$cmd installed"
    else
        fail "$cmd not found"
    fi
done

if docker compose version &>/dev/null; then
    pass "docker compose available"
else
    fail "docker compose not available (need v2+)"
fi

if docker info &>/dev/null; then
    pass "Docker daemon running"
else
    fail "Docker daemon not running"
fi

echo ""

# ─── Port checks ─────────────────────────────────────────────────────────────
echo "--- Port availability (cloud) ---"

for port in "$GC_HOST_PORT" "$SPIRE_HOST_PORT"; do
    if ! ss -tlnp 2>/dev/null | grep -q ":${port} " && \
       ! netstat -tlnp 2>/dev/null | grep -q ":${port} "; then
        pass "Port $port is free"
    else
        warn "Port $port may already be in use"
    fi
done

echo ""

# ─── Harbor connectivity ─────────────────────────────────────────────────────
echo "--- Harbor connectivity ---"

if curl -sf --connect-timeout 5 "${HARBOR_URL}/api/v2.0/systeminfo" &>/dev/null; then
    pass "Harbor reachable at $HARBOR_URL"
else
    fail "Cannot reach Harbor at $HARBOR_URL"
fi

HTTP_CODE=$(curl -so /dev/null -w '%{http_code}' --connect-timeout 5 \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_URL}/api/v2.0/users" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "200" ]; then
    pass "Harbor credentials valid"
else
    fail "Harbor credentials invalid (HTTP $HTTP_CODE)"
fi

# Check demo image exists in Harbor
MANIFEST_CODE=$(curl -so /dev/null -w '%{http_code}' --connect-timeout 5 \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    -H "Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json" \
    "${HARBOR_URL}/v2/${DEMO_IMAGE}/manifests/${DEMO_TAG}" 2>/dev/null || echo "000")
if [ "$MANIFEST_CODE" = "200" ]; then
    pass "Image ${DEMO_IMAGE}:${DEMO_TAG} exists in Harbor"
else
    fail "Image ${DEMO_IMAGE}:${DEMO_TAG} not found in Harbor (HTTP $MANIFEST_CODE). Push it first."
fi

echo ""

# ─── Satellite (edge) connectivity ───────────────────────────────────────────
echo "--- Edge device connectivity ---"

if ping -c 1 -W 3 "$SAT_IP" &>/dev/null; then
    pass "Satellite device reachable at $SAT_IP"
else
    fail "Cannot ping satellite device at $SAT_IP"
fi

if ssh -o ConnectTimeout=5 -o BatchMode=yes "${SAT_USER}@${SAT_IP}" "echo ok" &>/dev/null; then
    pass "SSH to ${SAT_USER}@${SAT_IP} works (key-based)"
else
    if ssh -o ConnectTimeout=5 "${SAT_USER}@${SAT_IP}" "echo ok" </dev/null &>/dev/null; then
        warn "SSH works but requires password (key-based auth recommended)"
    else
        fail "Cannot SSH to ${SAT_USER}@${SAT_IP}"
    fi
fi

# Check edge UID
REMOTE_UID=$(ssh -o ConnectTimeout=5 "${SAT_USER}@${SAT_IP}" "id -u" 2>/dev/null || echo "unknown")
if [ "$REMOTE_UID" = "$SAT_UID" ]; then
    pass "Remote UID matches ($SAT_UID)"
elif [ "$REMOTE_UID" = "unknown" ]; then
    warn "Could not verify remote UID"
else
    warn "Remote UID is $REMOTE_UID, expected $SAT_UID"
fi

# Check edge architecture
REMOTE_ARCH=$(ssh -o ConnectTimeout=5 "${SAT_USER}@${SAT_IP}" "uname -m" 2>/dev/null || echo "unknown")
if [ "$REMOTE_ARCH" != "unknown" ]; then
    pass "Edge architecture: $REMOTE_ARCH"
else
    warn "Could not detect edge architecture"
fi

echo ""

# ─── Summary ─────────────────────────────────────────────────────────────────
echo "============================================="
echo "  Results: $PASS passed, $FAIL failed, $WARN warnings"
echo "============================================="

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "Fix the failures above before running master-demo.sh"
    exit 1
else
    echo ""
    echo "All checks passed. Ready to run master-demo.sh"
    exit 0
fi
