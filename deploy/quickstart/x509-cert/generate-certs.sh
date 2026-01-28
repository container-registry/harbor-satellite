#!/bin/bash
# Generate X.509 certificates for SPIRE X.509 PoP attestation
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="$SCRIPT_DIR/certs"

mkdir -p "$CERTS_DIR"

if [ -f "$CERTS_DIR/ca.crt" ] && [ -f "$CERTS_DIR/agent-gc.crt" ]; then
    echo "Certificates already exist, skipping generation"
    exit 0
fi

echo "Generating certificates for X.509 PoP attestation..."

# Generate SPIRE CA
echo "Generating SPIRE CA..."
openssl genrsa -out "$CERTS_DIR/ca.key" 4096
openssl req -new -x509 -days 365 -key "$CERTS_DIR/ca.key" -out "$CERTS_DIR/ca.crt" \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE CA"

# Generate X.509 PoP CA (separate CA for agent certificates)
echo "Generating X.509 PoP CA..."
openssl genrsa -out "$CERTS_DIR/x509pop-ca.key" 4096
openssl req -new -x509 -days 365 -key "$CERTS_DIR/x509pop-ca.key" -out "$CERTS_DIR/x509pop-ca.crt" \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=X509 PoP CA"

# Generate Ground Control Agent certificate
echo "Generating Ground Control agent certificate..."
openssl genrsa -out "$CERTS_DIR/agent-gc.key" 2048
openssl req -new -key "$CERTS_DIR/agent-gc.key" -out "$CERTS_DIR/agent-gc.csr" \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=agent-gc"

cat > "$CERTS_DIR/agent-gc.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names
[alt_names]
URI.1 = spiffe://harbor-satellite.local/agent/ground-control
EOF

openssl x509 -req -days 365 -in "$CERTS_DIR/agent-gc.csr" \
    -CA "$CERTS_DIR/x509pop-ca.crt" -CAkey "$CERTS_DIR/x509pop-ca.key" -CAcreateserial \
    -out "$CERTS_DIR/agent-gc.crt" -extfile "$CERTS_DIR/agent-gc.ext"

# Generate Satellite Agent certificate
echo "Generating Satellite agent certificate..."
openssl genrsa -out "$CERTS_DIR/agent-satellite.key" 2048
openssl req -new -key "$CERTS_DIR/agent-satellite.key" -out "$CERTS_DIR/agent-satellite.csr" \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=agent-satellite"

cat > "$CERTS_DIR/agent-satellite.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names
[alt_names]
URI.1 = spiffe://harbor-satellite.local/agent/satellite
EOF

openssl x509 -req -days 365 -in "$CERTS_DIR/agent-satellite.csr" \
    -CA "$CERTS_DIR/x509pop-ca.crt" -CAkey "$CERTS_DIR/x509pop-ca.key" -CAcreateserial \
    -out "$CERTS_DIR/agent-satellite.crt" -extfile "$CERTS_DIR/agent-satellite.ext"

# Set permissions
chmod 600 "$CERTS_DIR"/*.key
chmod 644 "$CERTS_DIR"/*.crt

# Clean up CSR and ext files
rm -f "$CERTS_DIR"/*.csr "$CERTS_DIR"/*.ext "$CERTS_DIR"/*.srl

echo ""
echo "Certificates generated in $CERTS_DIR:"
ls -la "$CERTS_DIR"
