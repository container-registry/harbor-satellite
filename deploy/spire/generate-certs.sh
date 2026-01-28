#!/bin/bash
# Generate CA certificates for SPIRE development/testing
# Run this script once to create the initial CA certificates

set -e

CERTS_DIR="$(dirname "$0")/certs"
mkdir -p "$CERTS_DIR"

# Check if certs already exist
if [ -f "$CERTS_DIR/ca.crt" ] && [ -f "$CERTS_DIR/ca.key" ]; then
    echo "Certificates already exist in $CERTS_DIR"
    echo "Delete the certs directory to regenerate."
    exit 0
fi

echo "Generating CA certificates for SPIRE..."

# Generate CA key
openssl genrsa -out "$CERTS_DIR/ca.key" 4096

# Generate CA certificate
openssl req -new -x509 -days 365 -key "$CERTS_DIR/ca.key" \
    -out "$CERTS_DIR/ca.crt" \
    -subj "/C=US/ST=California/L=SanFrancisco/O=HarborSatellite/CN=SPIRE CA"

# Generate x509pop CA (same as main CA for development)
cp "$CERTS_DIR/ca.crt" "$CERTS_DIR/x509pop-ca.crt"

# Generate satellite fleet certificate for x509pop attestation (lazy mode)
openssl genrsa -out "$CERTS_DIR/satellite-fleet.key" 2048
openssl req -new -key "$CERTS_DIR/satellite-fleet.key" \
    -out "$CERTS_DIR/satellite-fleet.csr" \
    -subj "/C=US/ST=California/L=SanFrancisco/O=HarborSatellite/CN=satellite-fleet"
openssl x509 -req -in "$CERTS_DIR/satellite-fleet.csr" \
    -CA "$CERTS_DIR/ca.crt" -CAkey "$CERTS_DIR/ca.key" \
    -CAcreateserial -out "$CERTS_DIR/satellite-fleet.crt" -days 365

# Set permissions
chmod 600 "$CERTS_DIR"/*.key
chmod 644 "$CERTS_DIR"/*.crt

echo "Certificates generated successfully in $CERTS_DIR"
echo ""
echo "Files created:"
ls -la "$CERTS_DIR"
