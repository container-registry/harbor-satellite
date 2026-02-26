#!/bin/bash
# cleanup-demo.sh
# Tears down everything created by master-demo.sh
set -euo pipefail

# ─── Configuration (must match master-demo.sh) ──────────────────────────────
CLOUD_IP="10.147.106.55"
HARBOR_URL="http://${CLOUD_IP}:8080"
HARBOR_USERNAME="admin"
HARBOR_PASSWORD="Harbor12345"
SAT_USER="sat-1"
SAT_IP="10.147.106.144"
SAT_NAME="us-east-1"
WORK_DIR="$HOME/quickstart"

BOLD="\033[1m"
RED="\033[31m"
CYAN="\033[36m"
RESET="\033[0m"

step() { echo -e "\n${BOLD}${RED}===> $1${RESET}"; }
info() { echo -e "     ${CYAN}$1${RESET}"; }

remote() {
    ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
        "${SAT_USER}@${SAT_IP}" "$@"
}

echo -e "${BOLD}${RED}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║          Harbor Satellite Demo - CLEANUP                     ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${RESET}"
echo "This will destroy all demo resources on both cloud and edge."
echo "Press Enter to continue, or Ctrl+C to abort..."
read -r

# ═══════════════════════════════════════════════════════════════════════════════
# EDGE DEVICE CLEANUP
# ═══════════════════════════════════════════════════════════════════════════════

step "Cleaning up edge device (${SAT_USER}@${SAT_IP})"

info "Running all edge cleanup in a single SSH session..."
remote bash -s << 'EDGE_EOF'
echo "  Stopping satellite process..."
pkill -f harbor-satellite 2>/dev/null || true
echo "  Stopping SPIRE agent..."
pkill -f spire-agent 2>/dev/null || true
sleep 1
echo "  Removing satellite working directory..."
rm -rf ~/quickstart
echo "  Removing satellite config and Zot data..."
rm -rf ~/.config/satellite
echo "  Removing SPIRE agent socket..."
sudo rm -rf /tmp/spire-agent 2>/dev/null || true
echo "  Removing log files..."
rm -f /tmp/spire-agent.log /tmp/satellite.log
echo "  Removing k3s mirror config..."
sudo rm -f /etc/rancher/k3s/registries.yaml 2>/dev/null || true
echo "  Deleting demo k3s resources..."
sudo k3s kubectl delete pod satellite-mirror-test --ignore-not-found=true 2>/dev/null || true
sudo k3s kubectl delete namespace voting-app --ignore-not-found=true 2>/dev/null || true
sudo k3s kubectl delete namespace nginx --ignore-not-found=true 2>/dev/null || true
echo "  Pruning k3s cached images..."
sudo k3s crictl rmi --prune 2>/dev/null || true
echo "  Removing Zot registry storage..."
rm -rf /tmp/zot 2>/dev/null || true
rm -rf ~/zot 2>/dev/null || true
echo "  Restarting k3s to reset mirror config..."
sudo systemctl restart k3s 2>/dev/null || true
echo "  Edge cleanup done."
EDGE_EOF

info "Edge device cleaned up."

# ═══════════════════════════════════════════════════════════════════════════════
# CLOUD SIDE CLEANUP
# ═══════════════════════════════════════════════════════════════════════════════

step "Deleting Harbor robot account for satellite"

info "Looking for robot account matching '${SAT_NAME}'..."
ROBOT_ID=$(curl -sk -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_URL}/api/v2.0/robots" 2>/dev/null \
    | jq -r ".[] | select(.name | test(\"${SAT_NAME}\")) | .id // empty" 2>/dev/null | head -1)

if [ -n "$ROBOT_ID" ]; then
    HTTP_CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X DELETE \
        -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
        "${HARBOR_URL}/api/v2.0/robots/${ROBOT_ID}")
    if [ "$HTTP_CODE" = "200" ]; then
        info "Deleted robot account (ID: ${ROBOT_ID})"
    else
        info "Failed to delete robot account (HTTP $HTTP_CODE)"
    fi
else
    info "No robot account found for '${SAT_NAME}', skipping."
fi

# ─── Also delete the satellite project if it was auto-created ────────────────
info "Checking for 'satellite' project in Harbor..."
SAT_PROJECT_CODE=$(curl -sk -o /dev/null -w '%{http_code}' \
    -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_URL}/api/v2.0/projects/satellite" 2>/dev/null)

if [ "$SAT_PROJECT_CODE" = "200" ]; then
    DEL_CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X DELETE \
        -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
        "${HARBOR_URL}/api/v2.0/projects/satellite")
    if [ "$DEL_CODE" = "200" ]; then
        info "Deleted 'satellite' project from Harbor"
    else
        info "Could not delete 'satellite' project (HTTP $DEL_CODE) - may have artifacts"
    fi
else
    info "No 'satellite' project found, skipping."
fi

step "Cleaning up cloud side"

if [ -d "$WORK_DIR/gc" ]; then
    cd "$WORK_DIR/gc"

    info "Stopping all Docker Compose services and removing volumes..."
    docker compose down -v --remove-orphans 2>/dev/null || true

    info "Removing Docker network..."
    docker network rm harbor-satellite 2>/dev/null || true

    info "Removing SPIRE server data volume..."
    docker volume rm gc_spire-server-data 2>/dev/null || true

    cd "$HOME"
else
    info "No cloud working directory found at $WORK_DIR/gc, skipping compose cleanup."

    # Still try to clean up containers/volumes in case they exist
    info "Checking for leftover containers..."
    for c in ground-control spire-agent-gc spire-server harbor-satellite-postgres; do
        docker rm -f "$c" 2>/dev/null && info "  Removed container: $c" || true
    done

    info "Checking for leftover volumes..."
    for v in gc_postgres-data gc_spire-server-data gc_spire-server-socket gc_spire-agent-gc-data gc_spire-agent-gc-socket; do
        docker volume rm "$v" 2>/dev/null && info "  Removed volume: $v" || true
    done

    docker network rm harbor-satellite 2>/dev/null || true
fi

info "Removing quickstart directory..."
rm -rf "$WORK_DIR"

info "Cloud side cleaned up."

# ═══════════════════════════════════════════════════════════════════════════════
# DONE
# ═══════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}${CYAN}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║                  Cleanup Complete!                           ║"
echo "╠═══════════════════════════════════════════════════════════════╣"
echo "║  Removed:                                                   ║"
echo "║    - Satellite process + SPIRE agent on Pi                  ║"
echo "║    - All certs, configs, data on Pi                         ║"
echo "║    - Zot registry storage on Pi                             ║"
echo "║    - k3s cached images (crictl rmi --prune)                 ║"
echo "║    - k3s namespaces (voting-app, nginx)                     ║"
echo "║    - Docker Compose services (GC, SPIRE, Postgres)          ║"
echo "║    - Docker volumes and network                             ║"
echo "║    - $WORK_DIR directory                          ║"
echo "║                                                             ║"
echo "║  NOT removed:                                               ║"
echo "║    - SPIRE agent binary on Pi (/usr/local/bin/spire-agent)  ║"
echo "║    - Harbor (managed separately)                            ║"
echo "║    - SSH keys                                               ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${RESET}"
