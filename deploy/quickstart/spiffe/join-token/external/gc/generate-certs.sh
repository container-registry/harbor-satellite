#!/bin/bash
# Generate bootstrap CA certificate for SPIRE agent trust bundle
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="$SCRIPT_DIR/certs"

mkdir -p "$CERTS_DIR"

if [ -f "$CERTS_DIR/ca.crt" ] && [ -f "$CERTS_DIR/ca.key" ]; then
    echo "Certificates already exist, skipping generation"
    exit 0
fi

echo "Generating bootstrap CA certificate..."

openssl genrsa -out "$CERTS_DIR/ca.key" 4096

openssl req -new -x509 -days 365 -key "$CERTS_DIR/ca.key" -out "$CERTS_DIR/ca.crt" \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE CA"

chmod 600 "$CERTS_DIR/ca.key"
chmod 644 "$CERTS_DIR/ca.crt"

echo "CA certificate generated in $CERTS_DIR"
