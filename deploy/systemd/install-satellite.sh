#!/usr/bin/env bash
set -euo pipefail

## Harbor Satellite systemd Service Installation Script
## Usage: sudo ./install-satellite.sh <path-to-satellite-binary>

BINARY_PATH="${1:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

## Installation paths
INSTALL_BIN_DIR="/opt/harbor-satellite"
INSTALL_CONFIG_DIR="/etc/harbor-satellite"
INSTALL_DATA_DIR="/var/lib/harbor-satellite"
SYSTEMD_UNIT_DIR="/etc/systemd/system"

## Service configuration
SERVICE_USER="harbor-satellite"
SERVICE_GROUP="harbor-satellite"
SERVICE_FILE="harbor-satellite.service"

## Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

validate_binary() {
    if [[ -z "$BINARY_PATH" ]]; then
        log_error "Usage: $0 <path-to-satellite-binary>"
        exit 1
    fi

    if [[ ! -f "$BINARY_PATH" ]]; then
        log_error "Binary not found: $BINARY_PATH"
        exit 1
    fi

    if [[ ! -x "$BINARY_PATH" ]]; then
        log_error "Binary is not executable: $BINARY_PATH"
        exit 1
    fi

    log_info "Binary validated: $BINARY_PATH"
}

create_user() {
    if id "$SERVICE_USER" &>/dev/null; then
        log_info "User $SERVICE_USER already exists"
    else
        log_info "Creating system user: $SERVICE_USER"
        useradd --system --no-create-home --user-group --shell /usr/sbin/nologin "$SERVICE_USER"
    fi
}

create_directories() {
    log_info "Creating installation directories"

    ## Binary directory (root owned)
    mkdir -p "$INSTALL_BIN_DIR"
    chown root:root "$INSTALL_BIN_DIR"
    chmod 755 "$INSTALL_BIN_DIR"

    ## Config directory (root owned, group readable by service)
    mkdir -p "$INSTALL_CONFIG_DIR"
    chown root:root "$INSTALL_CONFIG_DIR"
    chmod 750 "$INSTALL_CONFIG_DIR"

    ## Data directory (service user owned - runtime state)
    mkdir -p "$INSTALL_DATA_DIR"
    chown "$SERVICE_USER:$SERVICE_GROUP" "$INSTALL_DATA_DIR"
    chmod 750 "$INSTALL_DATA_DIR"

    ## Drop-in directory for systemd overrides
    mkdir -p "$SYSTEMD_UNIT_DIR/harbor-satellite.service.d"
    chown root:root "$SYSTEMD_UNIT_DIR/harbor-satellite.service.d"
    chmod 755 "$SYSTEMD_UNIT_DIR/harbor-satellite.service.d"
}

install_binary() {
    log_info "Installing binary to $INSTALL_BIN_DIR/satellite"
    install -m 755 -o root -g root "$BINARY_PATH" "$INSTALL_BIN_DIR/satellite"
}

install_service_file() {
    log_info "Installing systemd service file"

    if [[ ! -f "$SCRIPT_DIR/$SERVICE_FILE" ]]; then
        log_error "Service file not found: $SCRIPT_DIR/$SERVICE_FILE"
        exit 1
    fi

    install -m 644 -o root -g root "$SCRIPT_DIR/$SERVICE_FILE" "$SYSTEMD_UNIT_DIR/$SERVICE_FILE"
}

install_env_template() {
    local env_template="$SCRIPT_DIR/examples/satellite.env.example"
    local env_dest="$INSTALL_CONFIG_DIR/satellite.env"

    if [[ -f "$env_dest" ]]; then
        log_warn "Configuration file already exists: $env_dest"
        log_warn "Not overwriting. New template available at: $env_template"
    else
        log_info "Installing environment template to $env_dest"
        install -m 640 -o root -g "$SERVICE_GROUP" "$env_template" "$env_dest"
        log_warn "IMPORTANT: Edit $env_dest and set GROUND_CONTROL_URL and TOKEN"
    fi
}

reload_systemd() {
    log_info "Reloading systemd daemon"
    systemctl daemon-reload
}

show_next_steps() {
    echo ""
    log_info "Installation complete!"
    echo ""
    echo "Next steps:"
    echo "  1. Configure the service:"
    echo "     sudo vim $INSTALL_CONFIG_DIR/satellite.env"
    echo "     (Set GROUND_CONTROL_URL and TOKEN or SPIFFE variables)"
    echo ""
    echo "  2. (Optional) Install drop-in overrides from $SCRIPT_DIR/examples/"
    echo "     - For SPIFFE auth: copy 10-spire-dependency.conf"
    echo "     - For CRI mirroring: copy 20-cri-mirroring.conf and 30-mirrors-*.conf"
    echo "     sudo cp $SCRIPT_DIR/examples/10-spire-dependency.conf $SYSTEMD_UNIT_DIR/harbor-satellite.service.d/"
    echo "     sudo systemctl daemon-reload"
    echo ""
    echo "  3. Enable and start the service:"
    echo "     sudo systemctl enable harbor-satellite.service"
    echo "     sudo systemctl start harbor-satellite.service"
    echo ""
    echo "  4. Check status:"
    echo "     sudo systemctl status harbor-satellite.service"
    echo "     sudo journalctl -u harbor-satellite.service -f"
    echo ""
    echo "Service files:"
    echo "  - Binary: $INSTALL_BIN_DIR/satellite"
    echo "  - Config: $INSTALL_CONFIG_DIR/satellite.env"
    echo "  - Data: $INSTALL_DATA_DIR/"
    echo "  - Service: $SYSTEMD_UNIT_DIR/$SERVICE_FILE"
    echo "  - Drop-ins: $SYSTEMD_UNIT_DIR/harbor-satellite.service.d/"
    echo ""
}

main() {
    log_info "Harbor Satellite systemd Installation"
    echo ""

    check_root
    validate_binary
    create_user
    create_directories
    install_binary
    install_service_file
    install_env_template
    reload_systemd
    show_next_steps
}

main "$@"
