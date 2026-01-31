#!/bin/bash
# Generate SSH CA and host certificates for SPIRE SSH PoP attestation
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="$SCRIPT_DIR/certs"

mkdir -p "$CERTS_DIR"

if [ -f "$CERTS_DIR/ssh-ca" ] && [ -f "$CERTS_DIR/agent-gc-host-key-cert.pub" ]; then
    echo "Certificates already exist, skipping generation"
    exit 0
fi

echo "Generating SSH certificates for SSH PoP attestation..."

# 1. Generate SSH CA key pair
echo "Generating SSH CA..."
ssh-keygen -t ed25519 -f "$CERTS_DIR/ssh-ca" -N "" -C "harbor-satellite-ssh-ca"

# 2. Generate bootstrap trust bundle (self-signed X.509 for SPIRE server trust)
echo "Generating bootstrap trust bundle..."
openssl genrsa -out "$CERTS_DIR/bootstrap.key" 4096
openssl req -new -x509 -days 365 -key "$CERTS_DIR/bootstrap.key" -out "$CERTS_DIR/bootstrap.crt" \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE Bootstrap CA"

# 3. Generate GC agent host key and sign with SSH CA
echo "Generating Ground Control agent host key..."
ssh-keygen -t ed25519 -f "$CERTS_DIR/agent-gc-host-key" -N "" -C "agent-gc"
ssh-keygen -s "$CERTS_DIR/ssh-ca" -I "agent-gc" -h -n "spire-agent-gc" \
    -V "+52w" "$CERTS_DIR/agent-gc-host-key.pub"

# 4. Generate satellite agent host key and sign with SSH CA
echo "Generating Satellite agent host key..."
ssh-keygen -t ed25519 -f "$CERTS_DIR/agent-satellite-host-key" -N "" -C "agent-satellite"
ssh-keygen -s "$CERTS_DIR/ssh-ca" -I "agent-satellite" -h -n "spire-agent-satellite" \
    -V "+52w" "$CERTS_DIR/agent-satellite-host-key.pub"

# Set permissions - private keys must be 600 (ssh-keygen -s requires this for the CA key)
chmod 600 "$CERTS_DIR/ssh-ca" "$CERTS_DIR/bootstrap.key"
chmod 644 "$CERTS_DIR/agent-gc-host-key" "$CERTS_DIR/agent-satellite-host-key"
chmod 644 "$CERTS_DIR/ssh-ca.pub" "$CERTS_DIR"/*.pub "$CERTS_DIR/bootstrap.crt"

echo "SSH certificates generated in $CERTS_DIR"
