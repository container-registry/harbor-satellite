# FIPS 140-2/140-3 Compliance Audit

## Overview
This document serves as an initial audit of cryptographic algorithms used across the Harbor Satellite codebase to evaluate compliance with FIPS (Federal Information Processing Standards) 140-2 and 140-3. FIPS compliance is critical for deployments in regulated environments such as federal government, healthcare, and financial sectors.

This audit maps current usages, determines their FIPS approval status, and identifies non-compliant algorithms along with recommended alternatives for future hardening.

## Cryptographic Inventory

| File Path | Function/Module | Algorithm | FIPS Approval Status | Reference Standard |
|-----------|-----------------|-----------|----------------------|--------------------|
| `internal/crypto/aes_provider.go` | `Encrypt` / `Decrypt` | AES-256-GCM | ✅ Approved | NIST SP 800-38D |
| `internal/crypto/aes_provider.go` | `Sign` / `Verify` | ECDSA P-256 + SHA-256 | ✅ Approved | FIPS 186-4 |
| `internal/crypto/aes_provider.go` | `GenerateKeyPair` | ECDSA P-256 | ✅ Approved | FIPS 186-4 |
| `internal/crypto/aes_provider.go` | `Hash` / `ensureKeySize` | SHA-256 | ✅ Approved | FIPS 180-4 |
| `internal/crypto/aes_provider.go` | `RandomBytes` | `crypto/rand` (OS CSPRNG) | ✅ Approved | NIST SP 800-90A |
| `internal/crypto/aes_provider.go` | `DeriveKey` | Argon2id | ❌ Not approved | — |
| `internal/secure/config.go` | Config Encryption | AES-256-GCM + Argon2id | ⚠️ Partial (AES OK, Argon2id No) | — |
| `internal/token/token.go` | Join Token Generation | `crypto/rand` | ✅ Approved | NIST SP 800-90A |
| `internal/identity/device_linux.go` | Device Fingerprint | SHA-256 | ✅ Approved | FIPS 180-4 |
| `internal/state/catalog.go` | Manifest Digest | SHA-256 | ✅ Approved | FIPS 180-4 |
| `internal/tls/config.go` | TLS Transport | TLS 1.2+ | ✅ Approved | NIST SP 800-52 Rev. 2 |
| `internal/spiffe/client.go` | mTLS via SPIFFE | TLS 1.2+ | ✅ Approved | NIST SP 800-52 Rev. 2 |
| `internal/crypto/provider_stub.go` | `NoOpProvider` (Stub) | None | ❌ N/A (Test/Dev only) | — |
| `ground-control/pkg/crypto/argon2.go` | `HashSecret` / `VerifySecret` | Argon2id | ❌ Not approved | — |
| `ground-control/internal/auth/password.go` | `HashPassword` | Argon2id | ❌ Not approved | — |
| `ground-control/internal/auth/password.go` | `GenerateSessionToken` | `crypto/rand` | ✅ Approved | NIST SP 800-90A |
| `ground-control/internal/server/helpers.go` | `GenerateRandomToken` | `crypto/rand` | ✅ Approved | NIST SP 800-90A |
| `ground-control/internal/server/helpers.go` | `hashRobotCredentials` | Argon2id | ❌ Not approved | — |
| `ground-control/main.go` | Server TLS | TLS 1.2+ | ✅ Approved | NIST SP 800-52 Rev. 2 |

## Non-FIPS Algorithms

### Argon2id
**Usage:**
Argon2id is currently utilized in multiple locations:
1. **Config Encryption:** Used as a Key Derivation Function (KDF) in `internal/crypto/aes_provider.go` and `internal/secure/config.go` to derive a symmetric encryption key from hardware fingerprinting.
2. **Password Hashing:** Used in `ground-control` (`ground-control/pkg/crypto/argon2.go`, `ground-control/internal/auth/password.go`, and `ground-control/internal/server/helpers.go`) to securely hash administrator passwords and robot account secrets.

**Compliance Context:**
While Argon2id is widely considered the industry best practice for password hashing due to its memory-hard properties, it is **not** currently approved by NIST for use in FIPS 140-validated modules.

**Recommended FIPS-Compliant Alternatives:**
* **For Password Hashing & Key Derivation from Secrets:** Use PBKDF2-HMAC-SHA-256 as specified in NIST SP 800-132.
* **For Key Derivation from High-Entropy Sources:** Use HKDF (HMAC-based Extract-and-Expand Key Derivation Function) with SHA-256 as specified in NIST SP 800-56C Rev. 2.

### NoOp Provider (Stub)
**Usage:**
Found in `internal/crypto/provider_stub.go`.
**Compliance Context:**
This returns data in plaintext without cryptographic operations. It is properly gated behind the `nospiffe` build tag and is used strictly for testing and development.
**Recommended Alternatives:**
No action required for production compliance, as long as the code continues to strictly restrict this provider from production builds.
