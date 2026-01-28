#!/bin/bash
# Generate CA certificates for SPIRE
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="$SCRIPT_DIR/certs"

mkdir -p "$CERTS_DIR"
mkdir -p "$SCRIPT_DIR/tokens"

if [ -f "$CERTS_DIR/ca.crt" ] && [ -f "$CERTS_DIR/ca.key" ]; then
    echo "Certificates already exist, skipping generation"
    exit 0
fi

echo "Generating CA certificates..."

# Generate CA private key
openssl genrsa -out "$CERTS_DIR/ca.key" 4096

# Generate CA certificate
openssl req -new -x509 -days 365 -key "$CERTS_DIR/ca.key" -out "$CERTS_DIR/ca.crt" \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE CA"

# Set permissions
chmod 600 "$CERTS_DIR/ca.key"
chmod 644 "$CERTS_DIR/ca.crt"

echo "CA certificates generated in $CERTS_DIR"
ls -la "$CERTS_DIR"
