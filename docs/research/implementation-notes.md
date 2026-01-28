# Implementation Notes

## Summary

This document covers the security implementation for Harbor Satellite zero-trust architecture.

## Phase 1 Foundation

Phase 1 implements the foundational security components.

## Test Coverage

All required test cases from test-strategy.md are implemented:
- Config Encryption: 8/8 tests
- Key Derivation: 4/4 tests
- Join Token: 6/6 tests
- Device Fingerprint: 5/5 tests
- TLS Setup: Certificate loading and validation

## Completed Components

### 1. CryptoProvider (internal/crypto)

**Files:**
- `provider.go` - Interface definition
- `aes_provider.go` - Production implementation
- `mock.go` - Mock for testing

**Implementation Details:**
- AES-256-GCM for symmetric encryption
- Argon2id for key derivation (OWASP recommended parameters)
- ECDSA P-256 for signing
- SHA-256 for hashing
- crypto/rand for secure random generation

**Key Decisions:**
- Argon2id chosen over bcrypt/scrypt for memory-hard properties
- GCM mode provides authenticated encryption (integrity + confidentiality)
- Keys shorter than 32 bytes are hashed with SHA-256 for consistency

### 2. DeviceIdentity (internal/identity)

**Files:**
- `device.go` - Interface definition
- `device_linux.go` - Linux implementation
- `mock.go` - Mock for testing

**Implementation Details:**
- Fingerprint combines: machine-id, MAC address, disk serial
- SHA-256 hash of combined components
- Graceful fallback when components unavailable
- Build-tagged for Linux (future: add darwin, windows)

**Sources:**
- `/etc/machine-id` - Persistent across reboots
- `/proc/cpuinfo` - CPU serial (when available)
- `/sys/class/block/*/device/serial` - Disk serial
- Network interfaces via net.Interfaces()

### 3. ConfigEncryptor (internal/secure)

**Files:**
- `config.go` - Encryption/decryption logic

**Implementation Details:**
- Derives encryption key from device fingerprint
- Uses random salt per encryption (16 bytes)
- Stores encrypted data as JSON with version, salt, data fields
- File permissions set to 0600

**Security Properties:**
- Config only decryptable on same device
- Different salt each encryption (prevents pattern analysis)
- Version field for future format upgrades

### 4. JoinToken (internal/token)

**Files:**
- `token.go` - Token generation and validation
- `store.go` - Token store for single-use enforcement

**Implementation Details:**
- Base64URL-encoded JSON tokens
- Contains: version, ID, expiry, ground control URL
- MemoryTokenStore tracks used tokens and rate limits

**Security Properties:**
- Single-use enforcement
- Expiration validation
- Ground Control URL binding
- Rate limiting per IP

## Test Coverage

All components have comprehensive unit tests covering:
- Success paths
- Error conditions
- Edge cases
- Roundtrip operations
- Security properties

### 5. TLS Config (internal/tls)

**Files:**
- `config.go` - TLS configuration and certificate loading

**Implementation Details:**
- Load certificates and keys from files
- Validate certificate expiry and validity period
- Load CA pools for trust chain
- Support client and server TLS configs
- mTLS support with client certificate verification

**Security Properties:**
- Minimum TLS 1.2 by default
- Certificate expiry validation
- CA-based trust chain

## Verification

Run the verification script:
```bash
go run ./cmd/verify-phase1/
```

Run all tests:
```bash
go test ./internal/crypto/... ./internal/identity/... ./internal/secure/... ./internal/token/... ./internal/tls/... -v
```

## Dependencies Added

- `golang.org/x/crypto` - Argon2id implementation (already in go.mod)

## Phase 2 Secure Communication

Phase 2 implements secure communication features between Satellite and Ground Control.

### Completed Components

### 1. TLS Integration (pkg/config, internal/state)

**Files:**
- `pkg/config/config.go` - TLSConfig struct
- `pkg/config/validate.go` - TLS validation
- `pkg/config/getters.go` - GetTLSConfig(), ShouldEncryptConfig()
- `internal/state/fetcher.go` - TLS for crane operations
- `internal/state/replicator.go` - TLS transport
- `internal/state/registration_process.go` - TLS HTTP client for ZTR

**Implementation Details:**
- TLS configuration via config file (cert_file, key_file, ca_file, skip_verify)
- Support for both client certificates (mTLS) and CA-only verification
- Integrated with crane for OCI registry operations
- TLS-enabled HTTP client for satellite registration

### 2. Token Expiry Validation (ground-control)

**Files:**
- `ground-control/sql/schema/008_token_expiry.sql` - Migration
- `ground-control/internal/server/satellite_handlers.go` - ZTR handler

**Implementation Details:**
- Added expires_at column to satellite_token table
- 24-hour default token expiry
- Expiry validation in ZTR handler
- Expired tokens return HTTP 401 Unauthorized

**Security Properties:**
- Time-limited tokens prevent replay attacks
- Tokens are single-use (deleted after successful registration)

### 3. Rate Limiting (ground-control)

**Files:**
- `ground-control/internal/middleware/ratelimit.go` - Rate limiter
- `ground-control/internal/server/routes.go` - Middleware integration

**Implementation Details:**
- IP-based rate limiting using sliding window algorithm
- 10 requests per minute per IP for ZTR endpoint
- Automatic cleanup of expired entries
- X-Forwarded-For and X-Real-IP header support for proxies

**Security Properties:**
- Prevents brute-force attacks on token endpoints
- Returns HTTP 429 Too Many Requests when limit exceeded

### 4. Certificate Rotation (ground-control)

**Files:**
- `ground-control/internal/middleware/certwatcher.go` - Certificate watcher
- `ground-control/internal/server/server.go` - Integration

**Implementation Details:**
- File modification time monitoring
- Automatic certificate reload when files change
- Uses tls.Config.GetCertificate for dynamic certificate selection
- 30-second check interval for file changes

**Security Properties:**
- Zero-downtime certificate rotation
- No server restart required for cert renewal

### 5. Ground Control TLS Server

**Files:**
- `ground-control/internal/server/server.go` - TLS server setup
- `ground-control/main.go` - Conditional HTTPS startup
- `ground-control/.env.example` - TLS environment variables

**Implementation Details:**
- TLS enabled via environment variables (TLS_CERT_FILE, TLS_KEY_FILE)
- Optional mTLS with client verification (TLS_CA_FILE)
- Minimum TLS 1.2

**Configuration:**
```bash
TLS_CERT_FILE=/path/to/server.crt
TLS_KEY_FILE=/path/to/server.key
TLS_CA_FILE=/path/to/ca.crt  # Optional, enables mTLS
```

## Verification

Run Ground Control tests:
```bash
cd ground-control && go test ./... -v
```

Build all modules:
```bash
go build ./...
cd ground-control && go build ./...
```

## Next Steps (Phase 3)

1. Audit logging for security events
2. Certificate management API
3. Token revocation mechanism
4. Health check for certificate expiry warnings
