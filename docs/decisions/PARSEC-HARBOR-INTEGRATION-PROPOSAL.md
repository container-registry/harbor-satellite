# PARSEC Integration Proposal for Harbor
## Using PARSEC as a Secure Backend for SSL/TLS, Identity, and Attestation Management

**Version:** 1.0  
**Date:** March 6, 2026  
**Authors:** Harbor-PARSEC Integration Team  

---

## Executive Summary

This proposal outlines the integration of **PARSEC** (Platform AbstRaction for SECurity) with **Harbor** to provide hardware-backed security for cryptographic operations, certificate management, and attestation. PARSEC will serve as a unified abstraction layer for accessing Hardware Security Modules (HSMs), Trusted Platform Modules (TPMs), and secure enclaves, enabling Harbor to leverage platform security capabilities without vendor lock-in.

**Key Benefits:**
- **Hardware-backed key storage**: Private keys never leave secure hardware
- **Platform abstraction**: Support for TPM, PKCS#11, HSMs, and secure enclaves through a single API
- **Enhanced security**: Cryptographic operations performed in trusted execution environments
- **Simplified certificate lifecycle**: Automated certificate generation and rotation with hardware-protected keys
- **Attestation support**: Enable workload identity and platform integrity verification
- **Cloud-native deployment**: Designed for containerized and multi-tenant environments

---

## 1. Background

### 1.1 Harbor's Current Security Architecture

Harbor currently manages TLS/SSL certificates through:
- File-based certificate storage (`tls.LoadX509KeyPair()`)
- Environment variable configuration for internal TLS
- Standard Go crypto libraries for cryptographic operations
- No native integration with hardware security modules

**Limitations:**
- Private keys stored on disk (vulnerable to extraction)
- No hardware-backed cryptographic operations
- Limited support for attestation and workload identity
- Manual certificate rotation processes
- No unified interface for different security backends

### 1.2 PARSEC Overview

**PARSEC** is an open-source platform abstraction for security services that provides:
- **Unified API**: Common interface (PSA Crypto API) for all hardware security backends
- **Multiple Providers**: Support for TPM, PKCS#11, Mbed Crypto, CryptoAuthLib, Trusted Service
- **Language Support**: Client libraries for Rust, Go, Java, Python, and C
- **Multi-tenancy**: Isolated key stores per application/user
- **Microservice Architecture**: Security as a service model
- **Client-Server Model**: Unix domain socket communication (IPC)

**Supported Backends:**hj
- **TPM Provider**: Trusted Platform Module (TPM 2.0)
- **PKCS#11 Provider**: Industry-standard interface for HSMs and smart cards
- **Mbed Crypto Provider**: Software cryptographic library (PSA Crypto implementation)
- **CryptoAuthLib Provider**: Microchip secure elements (ATECC508A/608A)
- **Trusted Service Provider**: ARM TrustZone secure world

---

## 2. Proposed Architecture

### 2.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Harbor Components                       │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐            │
│  │   Core     │  │  Registry  │  │ Job Service│  ...       │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘            │
│        │                │                │                   │
│        └────────────────┴────────────────┘                   │
│                         │                                    │
│                         ▼                                    │
│        ┌────────────────────────────────┐                   │
│        │  Harbor PARSEC Client Library  │                   │
│        │  (Go Client Integration Layer) │                   │
│        └────────────────┬───────────────┘                   │
└─────────────────────────┼───────────────────────────────────┘
                          │ Unix Socket
                          │ (/run/parsec/parsec.sock)
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                     PARSEC Service                           │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              Core Service (Rust)                      │  │
│  │  - Request Router                                     │  │
│  │  - Authentication (UnixPeerCredentials/Direct)        │  │
│  │  - Key Info Manager (SQLite/OnDisk)                   │  │
│  └──────────────────┬───────────────────────────────────┘  │
│                     │                                        │
│    ┌────────────────┼────────────────┬──────────────┐      │
│    ▼                ▼                 ▼              ▼      │
│  ┌────┐         ┌────────┐       ┌──────┐      ┌──────┐   │
│  │TPM │         │PKCS#11 │       │ Mbed │      │ T.S. │   │
│  │    │         │Provider│       │Crypto│      │      │   │
│  └─┬──┘         └───┬────┘       └──┬───┘      └──┬───┘   │
└────┼──────────────────┼──────────────┼─────────────┼───────┘
     │                  │               │             │
     ▼                  ▼               ▼             ▼
┌─────────┐      ┌──────────┐    ┌─────────┐   ┌─────────┐
│TPM 2.0  │      │HSM/Token │    │Software │   │TrustZone│
│Hardware │      │(PKCS#11) │    │Crypto   │   │ Secure  │
└─────────┘      └──────────┘    └─────────┘   └─────────┘
```

### 2.2 Component Description

#### 2.2.1 Harbor PARSEC Client Library

A Go library that wraps the PARSEC client to provide Harbor-specific functionality:

**Core Responsibilities:**
- Certificate generation and signing using PARSEC-managed keys
- TLS configuration with PARSEC-backed private keys
- Key lifecycle management (generation, rotation, deletion)
- Attestation quote generation and verification
- Identity management and authentication

**Key Interfaces:**
```go
type ParsecCertManager interface {
    // Generate a new key pair in PARSEC
    GenerateKeyPair(keyName string, algorithm KeyAlgorithm) error
    
    // Generate a self-signed certificate with PARSEC key
    GenerateSelfSignedCert(keyName string, subject pkix.Name) (*x509.Certificate, error)
    
    // Generate a CSR with PARSEC key
    GenerateCSR(keyName string, subject pkix.Name) (*x509.CertificateRequest, error)
    
    // Get TLS certificate backed by PARSEC
    GetTLSCertificate() (*tls.Certificate, error)
    
    // Rotate certificate with new key
    RotateCertificate(oldKeyName, newKeyName string) error
    
    // Sign data using PARSEC key
    Sign(keyName string, data []byte) ([]byte, error)
    
    // Verify signature using PARSEC key
    Verify(keyName string, data, signature []byte) error
}

type ParsecAttestationManager interface {
    // Generate attestation quote
    GetAttestationQuote(nonce []byte) (*AttestationQuote, error)
    
    // Verify attestation quote
    VerifyAttestationQuote(quote *AttestationQuote) error
    
    // Get platform identity
    GetPlatformIdentity() (*PlatformIdentity, error)
}
```

#### 2.2.2 PARSEC Service Configuration

The PARSEC service will be configured based on the deployment environment:

**Production Configuration (TPM Backend):**
```toml
[core_settings]
thread_pool_size = 8
log_level = "info"

[listener]
listener_type = "DomainSocket"
timeout = 200
socket_path = "/run/parsec/parsec.sock"

[authenticator]
auth_type = "UnixPeerCredentials"

[[key_manager]]
name = "sqlite-manager"
manager_type = "SQLite"
store_path = "/var/lib/parsec/kim-mappings/sqlite-key-info-manager.sqlite3"

[[provider]]
name = "tpm-provider"
provider_type = "Tpm"
key_info_manager = "sqlite-manager"
tcti = "device:/dev/tpmrm0"
owner_hierarchy_auth = "${TPM_OWNER_AUTH}"
```

**Development Configuration (Mbed Crypto Backend):**
```toml
[[provider]]
name = "mbed-crypto-provider"
provider_type = "MbedCrypto"
key_info_manager = "sqlite-manager"
```

---

## 3. Integration Points

### 3.1 TLS Certificate Management

**Current State:**
```go
// Harbor currently loads certificates from files
cert, err := tls.LoadX509KeyPair(crtPath, keyPath)
```

**PARSEC Integration:**
```go
// Initialize PARSEC client
parsecClient, err := parsec.CreateConfiguredClient("harbor-core")

// Generate key in PARSEC (one-time setup)
keyAttrs := parsec.KeyAttributes{
    KeyType: parsec.NewKeyType().RsaKeyPair(),
    KeyBits: 2048,
    Algorithm: algorithm.AsymmetricSignature.RsapkcsV15Sign(algorithm.Hash.Sha256()),
    Usage: parsec.UsageFlags{Sign: true},
}
err = parsecClient.PsaGenerateKey("harbor-tls-key", &keyAttrs)

// Generate certificate with PARSEC key
certManager := harbor_parsec.NewCertificateManager(parsecClient)
cert, err := certManager.GenerateSelfSignedCert("harbor-tls-key", subject)

// Get TLS config backed by PARSEC
tlsConfig, err := certManager.GetTLSConfig("harbor-tls-key", cert)
```

**Benefits:**
- Private key never leaves TPM/HSM
- TLS operations performed in hardware
- Automated key rotation without downtime
- Centralized key management

### 3.2 Image Signing (Notary/Cosign Integration)

**PARSEC-backed Image Signing:**
```go
// Generate signing key in PARSEC
parsecClient.PsaGenerateKey("harbor-content-trust-key", &signingKeyAttrs)

// Sign image digest
imageDigest := sha256.Sum256(imageManifest)
signature, err := parsecClient.PsaSignHash(
    "harbor-content-trust-key",
    imageDigest[:],
    &algorithm.AsymmetricSignature.RsaP256Sha256(),
)

// Signature verification
err = parsecClient.PsaVerifyHash(
    "harbor-content-trust-key",
    imageDigest[:],
    signature,
    &algorithm.AsymmetricSignature.RsaP256Sha256(),
)
```

**Benefits:**
- Content trust keys protected in hardware
- Non-repudiation for signed images
- Hardware-backed signature generation

### 3.3 Database Encryption

**Transparent Data Encryption (TDE) with PARSEC:**
```go
// Generate database encryption key in PARSEC
parsecClient.PsaGenerateKey("harbor-db-master-key", &aesKeyAttrs)

// Encrypt data encryption keys (DEK) with master key
encryptedDEK, err := parsecClient.PsaAeadEncrypt(
    "harbor-db-master-key",
    &algorithm.AeadWithDefaultLengthTag.AeadChacha20Poly1305(),
    nonce,
    additionalData,
    plaintextDEK,
)
```

**Benefits:**
- Master key never exposed
- Hardware-enforced key hierarchy
- Compliance with data protection regulations

### 3.4 API Token Signing

**PARSEC-backed JWT Signing:**
```go
// Sign JWT tokens with PARSEC
tokenClaims := jwt.MapClaims{
    "sub": userID,
    "exp": expiration,
    "iat": issuedAt,
}

tokenHash := sha256.Sum256([]byte(tokenClaims.String()))
signature, err := parsecClient.PsaSignHash(
    "harbor-jwt-signing-key",
    tokenHash[:],
    &algorithm.AsymmetricSignature.EcdsaP256Sha256(),
)
```

### 3.5 Attestation and Workload Identity

**TPM-based Attestation:**
```go
type AttestationManager struct {
    parsecClient *parsec.BasicClient
}

// Generate attestation quote for platform integrity
func (a *AttestationManager) GenerateAttestationQuote(nonce []byte) (*AttestationQuote, error) {
    // Use TPM provider to generate quote
    // Includes PCR measurements, firmware versions, etc.
    
    quote := &AttestationQuote{
        Nonce: nonce,
        PCRs: tpmPCRs,
        Signature: tpmQuoteSignature,
        Timestamp: time.Now(),
    }
    return quote, nil
}

// Verify attestation from another Harbor instance
func (a *AttestationManager) VerifyRemoteAttestation(quote *AttestationQuote) error {
    // Verify quote signature
    // Check PCR values against expected baseline
    // Validate certificate chain back to TPM root
    return nil
}
```

**Use Cases:**
- Registry replication authentication
- Multi-datacenter deployment verification
- Supply chain security (verify build environment integrity)

---

## 4. Implementation Roadmap

### Phase 1: Foundation (Months 1-2)

**Objectives:**
- Set up PARSEC service in Harbor deployment
- Develop Harbor PARSEC client library
- Implement basic key generation and storage

**Deliverables:**
- PARSEC service Docker container
- Go client library for PARSEC
- Documentation for deployment
- Unit tests

**Tasks:**
1. Create PARSEC service container image
2. Develop `harbor-parsec` Go package
3. Implement key generation operations
4. Add PARSEC configuration to Harbor installer
5. Write integration tests with Mbed Crypto provider

### Phase 2: TLS Integration (Months 3-4)

**Objectives:**
- Replace file-based certificate loading with PARSEC
- Implement certificate generation with PARSEC keys
- Support automatic certificate rotation

**Deliverables:**
- PARSEC-backed TLS configuration
- Certificate lifecycle management
- Migration tools for existing deployments
- Performance benchmarks

**Tasks:**
1. Implement `ParsecCertManager` interface
2. Integrate with Harbor's internal TLS configuration
3. Add certificate rotation automation
4. Create migration guide for existing installations
5. Performance testing and optimization

### Phase 3: Content Trust & Signing (Months 5-6)

**Objectives:**
- Integrate PARSEC with Notary/Cosign for image signing
- Implement hardware-backed signature verification
- Support multiple signing keys per project

**Deliverables:**
- PARSEC-backed image signing
- Updated Notary integration
- Signing key management UI
- Audit logging for signing operations

**Tasks:**
1. Implement image signing with PARSEC keys
2. Update Notary signer to use PARSEC
3. Add key management APIs
4. Update Harbor UI for key management
5. Implement audit trail for signing operations

### Phase 4: Advanced Features (Months 7-9)

**Objectives:**
- Implement attestation support
- Add database encryption
- Support for multiple PARSEC providers
- JWT/API token signing

**Deliverables:**
- Attestation API and verification
- Transparent data encryption
- Multi-provider configuration
- API token signing with PARSEC

**Tasks:**
1. Implement attestation manager
2. Add database encryption with PARSEC
3. Support TPM, PKCS#11, and other providers
4. Implement JWT signing with PARSEC keys
5. Documentation for advanced configurations

### Phase 5: Production Hardening (Months 10-12)

**Objectives:**
- Security audit
- Performance optimization
- High availability configuration
- Comprehensive documentation

**Deliverables:**
- Security audit report
- Performance tuning guide
- HA deployment architecture
- Operations manual

**Tasks:**
1. Conduct security audit
2. Optimize critical paths
3. Design HA PARSEC deployment
4. Create comprehensive documentation
5. Develop troubleshooting guides

---

## 5. Technical Requirements

### 5.1 Infrastructure Requirements

**PARSEC Service:**
- Linux kernel 5.4+ (for TPM 2.0 support)
- TPM 2.0 hardware (or TPM simulator for development)
- 100MB disk space for PARSEC service and key mappings
- Access to `/dev/tpmrm0` (or `/dev/tpm0` with exclusive access)

**Harbor Modifications:**
- Go 1.19+ (for PARSEC client library)
- Unix domain socket communication
- Additional container for PARSEC service (or sidecar pattern)

**Optional (Provider-specific):**
- HSM with PKCS#11 interface (for PKCS#11 provider)
- ARM TrustZone-enabled platform (for Trusted Service provider)
- Microchip secure element (for CryptoAuthLib provider)

### 5.2 Security Considerations

**Threat Model:**
- Protect against private key extraction
- Prevent unauthorized cryptographic operations
- Ensure key isolation between tenants
- Protect against privilege escalation

**Security Controls:**
- Unix file permissions on socket (0700)
- Authentication via UnixPeerCredentials
- Key ownership tied to application identity
- Audit logging for all operations
- Secure key deletion (zeroization)

**Compliance:**
- FIPS 140-2 compliance (with certified HSM)
- Common Criteria EAL4+ (with TPM 2.0)
- PCI-DSS compliance for key management
- GDPR compliance for data encryption

### 5.3 Performance Considerations

**Expected Performance:**
- Key generation: 500-2000ms (RSA 2048, TPM)
- Signing operation: 50-200ms (RSA, TPM)
- Verification: 5-20ms (RSA, software)
- TLS handshake overhead: +50-100ms (TPM-backed)

**Optimization Strategies:**
- Certificate caching (reduce signing operations)
- Batch signing operations
- Asynchronous key generation
- Connection pooling for PARSEC socket

**Benchmarking:**
- Baseline performance without PARSEC
- Performance with Mbed Crypto (software)
- Performance with TPM provider
- Performance with PKCS#11 HSM

---

## 6. Deployment Models

### 6.1 Docker Compose Deployment

```yaml
version: '3.8'

services:
  parsec:
    image: harbor-parsec:latest
    container_name: harbor-parsec
    volumes:
      - /dev/tpmrm0:/dev/tpmrm0
      - parsec-socket:/run/parsec
      - parsec-data:/var/lib/parsec
    devices:
      - /dev/tpmrm0
    cap_add:
      - SYS_ADMIN
    security_opt:
      - apparmor:unconfined

  harbor-core:
    image: goharbor/harbor-core:v2.x-parsec
    depends_on:
      - parsec
    volumes:
      - parsec-socket:/run/parsec:ro
    environment:
      - PARSEC_ENABLED=true
      - PARSEC_SOCKET=/run/parsec/parsec.sock
      - PARSEC_KEY_NAME=harbor-tls-key

volumes:
  parsec-socket:
  parsec-data:
```

### 6.2 Kubernetes Deployment

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: harbor-core
spec:
  serviceAccountName: harbor-core
  initContainers:
  - name: parsec-init
    image: harbor-parsec:latest
    command: ["/bin/init-parsec"]
    volumeMounts:
    - name: parsec-socket
      mountPath: /run/parsec
    - name: tpm
      mountPath: /dev/tpmrm0
  
  containers:
  - name: harbor-core
    image: goharbor/harbor-core:v2.x-parsec
    volumeMounts:
    - name: parsec-socket
      mountPath: /run/parsec
      readOnly: true
    env:
    - name: PARSEC_ENABLED
      value: "true"
  
  - name: parsec-sidecar
    image: harbor-parsec:latest
    volumeMounts:
    - name: parsec-socket
      mountPath: /run/parsec
    - name: tpm
      mountPath: /dev/tpmrm0
    securityContext:
      privileged: true
  
  volumes:
  - name: parsec-socket
    emptyDir: {}
  - name: tpm
    hostPath:
      path: /dev/tpmrm0
      type: CharDevice
```

### 6.3 High Availability Configuration

**Multi-instance PARSEC with Shared HSM:**
```
┌──────────────┐       ┌──────────────┐       ┌──────────────┐
│Harbor Node 1 │       │Harbor Node 2 │       │Harbor Node 3 │
│  ┌────────┐  │       │  ┌────────┐  │       │  ┌────────┐  │
│  │PARSEC  │  │       │  │PARSEC  │  │       │  │PARSEC  │  │
│  │Client  │  │       │  │Client  │  │       │  │Client  │  │
│  └───┬────┘  │       │  └───┬────┘  │       │  └───┬────┘  │
│      │       │       │      │       │       │      │       │
│  ┌───▼────┐  │       │  ┌───▼────┐  │       │  ┌───▼────┐  │
│  │PARSEC  │  │       │  │PARSEC  │  │       │  │PARSEC  │  │
│  │Service │  │       │  │Service │  │       │  │Service │  │
│  └───┬────┘  │       │  └───┬────┘  │       │  └───┬────┘  │
└──────┼───────┘       └──────┼───────┘       └──────┼───────┘
       │                      │                      │
       └──────────────────────┴──────────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │  Network HSM     │
                    │  (PKCS#11)       │
                    │  - Shared Keys   │
                    │  - HA Cluster    │
                    └──────────────────┘
```

---

## 7. Migration Strategy

### 7.1 Backward Compatibility

**Goals:**
- Zero-downtime migration
- Fallback to traditional certificate loading
- Gradual rollout capability

**Implementation:**
```go
func GetTLSCertificate() (*tls.Certificate, error) {
    if isParsecEnabled() {
        // Try PARSEC first
        cert, err := getParsecCertificate()
        if err == nil {
            return cert, nil
        }
        log.Warn("PARSEC certificate loading failed, falling back to file-based")
    }
    
    // Fallback to traditional file-based loading
    return tls.LoadX509KeyPair(certPath, keyPath)
}
```

### 7.2 Migration Steps

**Pre-Migration:**
1. Deploy PARSEC service alongside existing Harbor
2. Validate PARSEC service functionality
3. Generate new keys in PARSEC
4. Generate certificates with PARSEC keys (parallel to existing)

**Migration:**
1. Configure Harbor to use PARSEC for new connections
2. Monitor performance and errors
3. Gradually increase PARSEC usage percentage
4. Update all components to use PARSEC

**Post-Migration:**
1. Verify all cryptographic operations use PARSEC
2. Securely delete old file-based private keys
3. Remove fallback code paths
4. Document production configuration

### 7.3 Rollback Plan

**Rollback Triggers:**
- Performance degradation >50%
- Error rate increase >5%
- Critical security issue discovered

**Rollback Steps:**
1. Revert configuration to use file-based certificates
2. Restart Harbor components
3. Verify system stability
4. Investigate root cause

---

## 8. Testing Strategy

### 8.1 Unit Tests

- Key generation operations
- Certificate generation with PARSEC keys
- Signing and verification operations
- Error handling and edge cases
- Authentication and authorization

### 8.2 Integration Tests

- End-to-end TLS connection with PARSEC
- Certificate rotation workflow
- Multi-provider configuration
- Fallback to traditional certificates
- Performance benchmarking

### 8.3 Security Tests

- Key extraction attempts
- Privilege escalation attempts
- Socket permission validation
- Authentication bypass attempts
- Cryptographic operation auditing

### 8.4 Performance Tests

- TLS handshake latency
- Throughput under load
- Concurrent request handling
- Resource usage (CPU, memory, I/O)
- Scaling characteristics

---

## 9. Documentation Requirements

### 9.1 Operator Documentation

- Deployment guide (Docker Compose, Kubernetes, bare metal)
- Configuration reference
- Troubleshooting guide
- Performance tuning guide
- Security hardening checklist

### 9.2 Developer Documentation

- API reference for Harbor PARSEC library
- Integration examples
- Provider configuration guide
- Testing guide
- Contributing guide

### 9.3 Security Documentation

- Threat model
- Security controls
- Compliance mapping (FIPS, CC, PCI-DSS)
- Audit logging format
- Incident response procedures

---

## 10. Success Metrics

### 10.1 Security Metrics

- ✅ 100% of private keys stored in hardware
- ✅ 0 private key exposures
- ✅ 100% of cryptographic operations audited
- ✅ <1% false positive rate for attestation

### 10.2 Performance Metrics

- ✅ TLS handshake latency <500ms (p99)
- ✅ Signing operation latency <200ms (p99)
- ✅ No more than 20% throughput reduction vs. baseline
- ✅ Support for >1000 concurrent connections

### 10.3 Operational Metrics

- ✅ <1 hour deployment time (fresh installation)
- ✅ <30 minutes certificate rotation time
- ✅ 99.9% service availability
- ✅ Zero-downtime key rotation capability

---

## 11. Risks and Mitigation

### 11.1 Technical Risks

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| TPM unavailable in deployment environment | High | Medium | Fallback to Mbed Crypto provider |
| Performance overhead too high | High | Low | Extensive benchmarking, caching strategies |
| PARSEC service crashes | High | Low | Health monitoring, automatic restart |
| Socket permission issues | Medium | Medium | Clear documentation, validation scripts |
| Key migration failures | High | Low | Comprehensive migration testing, rollback plan |

### 11.2 Organizational Risks

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| User resistance to new deployment model | Medium | Medium | Clear benefits communication, gradual rollout |
| Insufficient expertise in TPM/HSM | Medium | High | Training programs, comprehensive documentation |
| Delayed timeline | Low | Medium | Phased approach, MVP focus |

---

## 12. Alternatives Considered

### 12.1 Direct TPM Integration

**Pros:**
- No additional service dependency
- Slightly lower latency

**Cons:**
- Vendor lock-in to TPM
- No abstraction for other HSMs
- Duplicate code across Harbor components
- Difficult to test without hardware

**Decision:** Rejected - PARSEC provides better abstraction

### 12.2 PKCS#11 Direct Integration

**Pros:**
- Industry standard
- Wide HSM support

**Cons:**
- C library integration in Go is complex
- No TPM support
- No multi-tenancy support
- Poor containerization support

**Decision:** Rejected - PARSEC provides better developer experience

### 12.3 Cloud KMS (AWS KMS, Azure Key Vault, Google Cloud KMS)

**Pros:**
- Managed service
- High availability
- Compliance certifications

**Cons:**
- Cloud vendor lock-in
- Network latency
- Cost at scale
- Not suitable for on-premises deployments

**Decision:** Rejected - PARSEC enables cloud-agnostic deployments

---

## 13. Conclusion

Integrating PARSEC with Harbor provides a robust, hardware-backed security foundation for certificate management, image signing, and attestation. The phased implementation approach allows for gradual adoption while maintaining backward compatibility. The platform abstraction offered by PARSEC ensures Harbor can leverage the best available security hardware across different deployment environments without vendor lock-in.

**Recommendation:** Proceed with Phase 1 implementation to validate the architecture and gather performance data before committing to full integration.

---

## 14. References

### PARSEC Documentation
- PARSEC Book: https://parallaxsecond.github.io/parsec-book/
- PARSEC GitHub: https://github.com/parallaxsecond/parsec
- PARSEC Go Client: https://github.com/parallaxsecond/parsec-client-go
- PSA Crypto API Specification: https://developer.arm.com/documentation/ihi0086/latest/

### Harbor Documentation
- Harbor Architecture: https://github.com/goharbor/harbor/wiki/Architecture-Overview-of-Harbor
- Harbor Documentation: https://goharbor.io/docs/

### Standards and Specifications
- TPM 2.0 Specification: https://trustedcomputinggroup.org/resource/tpm-library-specification/
- PKCS#11 Specification: http://docs.oasis-open.org/pkcs11/pkcs11-base/v2.40/
- PSA Certified: https://www.psacertified.org/

### Related Projects
- Notary: https://github.com/notaryproject/notary
- Cosign: https://github.com/sigstore/cosign
- Spire (SPIFFE implementation): https://spiffe.io/

---

## Appendix A: Code Examples

### Example: Complete Certificate Manager Implementation

```go
package harbor_parsec

import (
    "crypto"
    "crypto/rand"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "math/big"
    "time"
    
    "github.com/parallaxsecond/parsec-client-go/parsec"
    "github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

type CertificateManager struct {
    client *parsec.BasicClient
}

func NewCertificateManager(client *parsec.BasicClient) *CertificateManager {
    return &CertificateManager{client: client}
}

// GenerateKeyPair generates a new RSA key pair in PARSEC
func (cm *CertificateManager) GenerateKeyPair(keyName string, keyBits uint) error {
    attrs := &parsec.KeyAttributes{
        KeyType: parsec.NewKeyType().RsaKeyPair(),
        KeyBits: keyBits,
        Algorithm: algorithm.AsymmetricSignature.RsaPkcs1v15Sign(
            algorithm.Hash.Sha256(),
        ),
        Usage: parsec.UsageFlags{
            Sign:   true,
            Verify: true,
        },
    }
    
    return cm.client.PsaGenerateKey(keyName, attrs)
}

// GenerateSelfSignedCertificate creates a self-signed certificate with a PARSEC key
func (cm *CertificateManager) GenerateSelfSignedCertificate(
    keyName string,
    subject pkix.Name,
    dnsNames []string,
    validDays int,
) (*x509.Certificate, error) {
    // Export public key
    pubKeyBytes, err := cm.client.PsaExportPublicKey(keyName)
    if err != nil {
        return nil, err
    }
    
    // Parse public key
    pubKey, err := x509.ParsePKIXPublicKey(pubKeyBytes)
    if err != nil {
        return nil, err
    }
    
    // Create certificate template
    serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
    template := &x509.Certificate{
        SerialNumber: serialNumber,
        Subject:      subject,
        DNSNames:     dnsNames,
        NotBefore:    time.Now(),
        NotAfter:     time.Now().AddDate(0, 0, validDays),
        KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
    }
    
    // Create PARSEC signer
    signer := &ParsecSigner{
        client:  cm.client,
        keyName: keyName,
        pubKey:  pubKey,
    }
    
    // Create self-signed certificate
    certDER, err := x509.CreateCertificate(rand.Reader, template, template, pubKey, signer)
    if err != nil {
        return nil, err
    }
    
    return x509.ParseCertificate(certDER)
}

// GetTLSCertificate returns a tls.Certificate backed by PARSEC
func (cm *CertificateManager) GetTLSCertificate(keyName string, cert *x509.Certificate) (*tls.Certificate, error) {
    signer := &ParsecSigner{
        client:  cm.client,
        keyName: keyName,
        pubKey:  cert.PublicKey,
    }
    
    return &tls.Certificate{
        Certificate: [][]byte{cert.Raw},
        PrivateKey:  signer,
        Leaf:        cert,
    }, nil
}

// ParsecSigner implements crypto.Signer interface using PARSEC
type ParsecSigner struct {
    client  *parsec.BasicClient
    keyName string
    pubKey  crypto.PublicKey
}

func (ps *ParsecSigner) Public() crypto.PublicKey {
    return ps.pubKey
}

func (ps *ParsecSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
    // Use PARSEC to sign the digest
    alg := &algorithm.AsymmetricSignatureAlgorithm{
        RsaPkcs1v15Sign: &algorithm.Hash{Sha256: &algorithm.Sha256{}},
    }
    
    return ps.client.PsaSignHash(ps.keyName, digest, alg)
}
```

---

**END OF PROPOSAL**
