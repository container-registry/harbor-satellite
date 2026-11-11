#!/usr/bin/env bash
# Install Harbor Satellite as a systemd service.
# Usage: sudo ./install-satellite.sh <path-to-satellite-binary>
set -euo pipefail

BINARY_PATH="${1:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BIN_DIR="/opt/harbor-satellite"
CONFIG_DIR="/etc/harbor-satellite"
UNIT_DIR="/etc/systemd/system"
SERVICE_USER="harbor-satellite"

info() { printf '[INFO] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*" >&2; }
die() {
    printf '[ERROR] %s\n' "$*" >&2
    exit 1
}

[[ ${EUID} -eq 0 ]] || die "run as root (sudo)"
[[ -n "${BINARY_PATH}" ]] || die "usage: $0 <path-to-satellite-binary>"
[[ -f "${BINARY_PATH}" && -x "${BINARY_PATH}" ]] || die "not an executable file: ${BINARY_PATH}"

if id "${SERVICE_USER}" >/dev/null 2>&1; then
    info "user ${SERVICE_USER} already exists"
else
    info "creating system user ${SERVICE_USER}"
    useradd --system --no-create-home --user-group --shell /usr/sbin/nologin "${SERVICE_USER}"
fi

info "installing binary to ${BIN_DIR}/satellite"
install -d -m 0755 -o root -g root "${BIN_DIR}"
install -m 0755 -o root -g root "${BINARY_PATH}" "${BIN_DIR}/satellite"

info "installing unit files to ${UNIT_DIR}"
install -m 0644 -o root -g root "${SCRIPT_DIR}/harbor-satellite.service" "${UNIT_DIR}/"
install -m 0644 -o root -g root "${SCRIPT_DIR}/harbor-satellite@.service" "${UNIT_DIR}/"
install -d -m 0755 -o root -g root "${UNIT_DIR}/harbor-satellite.service.d"

install -d -m 0750 -o root -g root "${CONFIG_DIR}"
if [[ -e "${CONFIG_DIR}/satellite.env" ]]; then
    warn "${CONFIG_DIR}/satellite.env exists, leaving it alone"
else
    info "installing ${CONFIG_DIR}/satellite.env"
    install -m 0600 -o root -g root "${SCRIPT_DIR}/examples/satellite.env.example" "${CONFIG_DIR}/satellite.env"
fi

systemctl daemon-reload

cat <<MSG

Done. Next:
  1. Edit ${CONFIG_DIR}/satellite.env and set GROUND_CONTROL_URL,
     HARBOR_REGISTRY_URL and TOKEN (or the SPIFFE variables).
  2. Optionally copy drop-ins from ${SCRIPT_DIR}/examples/ to
     ${UNIT_DIR}/harbor-satellite.service.d/ and run systemctl daemon-reload.
  3. systemctl enable --now harbor-satellite.service
  4. journalctl -u harbor-satellite.service -f
MSG
