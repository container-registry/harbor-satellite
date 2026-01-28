# Secrets Management at the Edge

Research notes on how to handle secrets and credentials in edge computing environments.

## The Edge Secrets Problem

### Challenges

**1. Physical Access**
- Edge devices are physically accessible to unauthorized people
- Devices can be stolen, tampered with, or compromised
- Cannot rely on physical security like data centers

**2. Blast Radius**
- If one device is compromised, it must NOT compromise entire network
- Need per-device isolation
- Static shared secrets are catastrophic

**3. Offline Operation**
- Stores must function despite limited connectivity
- Cannot always reach central secrets vault
- Need local caching with security

**4. Scale**
- Thousands of edge locations
- Cannot deploy dedicated security teams
- Zero-touch provisioning required

---

## Akeyless: Retail Secrets Architecture

Source: [Akeyless - Secrets for Retail](https://www.akeyless.io/blog/reinventing-secrets-management-for-the-retail-industry/)

### Solution: Stateless Gateways

**Architecture**
- Lightweight, stateless gateway at each store
- NOT full vault cluster per location
- Outbound-only connections to SaaS backend
- No sensitive data stored locally on gateway

**Benefits**
- Minimal operational burden
- Scales to thousands of locations
- No cluster management per site

### Offline Resilience

**Local Encrypted Caching**
- Read-only encrypted cache for operation during connectivity loss
- Fast local secret retrieval without backend dependency
- Continues operations when disconnected

### Distributed Fragments Cryptography (DFC)

**Key Splitting**
- Encryption keys split across regions
- Keys never reconstructed during operations
- One fragment kept on-premises for zero-knowledge

**Blast Radius Control**
- Each POS holds isolated key fragments
- Compromise of one device limits breach impact
- No single point of secret exposure

### Architecture Pattern
```
┌─────────────────────────────────────────────────────┐
│                  Cloud (SaaS)                        │
│  ┌──────────────────────────────────────────────┐   │
│  │          Akeyless Control Plane              │   │
│  │  - Key Management                            │   │
│  │  - Policy Engine                             │   │
│  │  - Audit Logging                             │   │
│  └──────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
                         │
                         │ Outbound only
                         ▼
┌─────────────────────────────────────────────────────┐
│                  Edge (Store)                        │
│  ┌──────────────────────────────────────────────┐   │
│  │         Stateless Gateway                    │   │
│  │  - Local encrypted cache                     │   │
│  │  - No secrets stored                         │   │
│  │  - Key fragments only                        │   │
│  └──────────────────────────────────────────────┘   │
│                         │                            │
│                         ▼                            │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐         │
│  │  POS 1  │    │  POS 2  │    │  POS 3  │         │
│  └─────────┘    └─────────┘    └─────────┘         │
└─────────────────────────────────────────────────────┘
```

---

## Azure Connected Registry

Source: [Azure Container Registry](https://learn.microsoft.com/en-us/azure/container-registry/quickstart-deploy-connected-registry-iot-edge-cli)

### Managed Identities (Secretless)
- Use managed identities instead of service principals
- Credentials fully managed, rotated, protected by platform
- Avoids hard-coded credentials in source or config

### TLS Certificate-Based Trust
- Certificate Authority (cert-manager) signs TLS certificates
- Distributed to registry service and clients
- All entities authenticate each other
- Secure, trusted environment within cluster

### Traditional (Still Common)
- `kubectl create secret docker-registry`
- Not recommended for edge
- Static credentials with rotation burden

---

## HashiCorp Vault at Edge (Problems)

### Challenges
- Full cluster deployment per location is costly
- High operational burden
- Difficult to manage at scale
- Not designed for thousands of small deployments

### When Vault Makes Sense
- Larger edge deployments (regional hubs)
- Existing HashiCorp ecosystem
- Dedicated ops teams available

---

## Secretless Patterns for Edge

### Pattern 1: Workload Identity
- No static credentials
- Identity derived from attestation
- Short-lived tokens issued on demand
- Example: SPIFFE/SPIRE

### Pattern 2: Encrypted Config with Derived Keys
- Config encrypted at rest
- Key derived from device attributes
- Device fingerprint as key material
- No stored master key

### Pattern 3: Just-in-Time Credentials
- Credentials fetched when needed
- Short TTL (minutes to hours)
- Automatic rotation
- No persistent storage

### Pattern 4: Split Keys
- Key material split across domains
- Need multiple factors to reconstruct
- Limits blast radius of compromise

---

## Application to Harbor Satellite

### Current State (Problems)
```
config.json:
{
  "state_config": {
    "auth": {
      "username": "robot$satellite-xxx",  // PLAINTEXT!
      "password": "abc123..."              // PLAINTEXT!
    }
  }
}
```

### Target State

**Option A: Encrypted Config**
```
config.sealed:
  - Encrypted with device-derived key
  - Key material: MAC + CPU ID + boot ID
  - Decrypted only in memory
  - Never written plaintext
```

**Option B: No Credentials (Preferred)**
```
No config.json credentials at all:
  - Satellite has SVID (certificate)
  - Certificate used for mTLS auth
  - Auto-rotated by SPIRE agent
  - Nothing stored to steal
```

### Offline Handling

**With Encrypted Config**
- Encrypted credentials cached locally
- Can operate during disconnection
- Re-encrypted on rotation

**With SVIDs**
- SVIDs have configurable TTL
- Cached in memory
- Continue operating until TTL expires
- Re-attest on reconnection

---

## Security Comparison

| Approach | At Rest | In Transit | Rotation | Offline | Complexity |
|----------|---------|------------|----------|---------|------------|
| Plaintext config | CRITICAL | - | Manual | Works | Low |
| Encrypted config | OK | - | Manual | Works | Medium |
| Vault per-site | Good | Good | Auto | Complex | High |
| SPIFFE/SPIRE | Excellent | mTLS | Auto | Configurable | Medium |
| Gateway + cache | Good | TLS | Auto | Works | Medium |

---

## Recommendations for Harbor Satellite

### Phase 1: Encrypted Config
1. Derive encryption key from device fingerprint
2. Encrypt `state_config.auth` at rest
3. Decrypt in memory only
4. Delete plaintext after migration

### Phase 2: Certificate-Based Auth
1. Replace username/password with mTLS
2. Satellite presents certificate
3. Ground Control validates
4. Harbor accepts certificate auth

### Phase 3: SPIFFE Integration
1. Optional SPIRE agent
2. Full attestation capabilities
3. Automatic rotation
4. Enterprise-grade

---

## Sources

- [Akeyless Retail Secrets](https://www.akeyless.io/blog/reinventing-secrets-management-for-the-retail-industry/)
- [Azure Connected Registry](https://learn.microsoft.com/en-us/azure/container-registry/quickstart-deploy-connected-registry-iot-edge-cli)
- [Azure Container Registry Auth](https://learn.microsoft.com/en-us/azure/container-registry/container-registry-authentication)
