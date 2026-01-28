# Zero-Trust & Workload Identity Research

Research notes on zero-trust architecture and workload identity frameworks for edge computing.

## Cloudflare Zero Trust Principles

Source: [Cloudflare Zero Trust](https://www.cloudflare.com/learning/security/glossary/what-is-zero-trust/)

### Core Tenets

**1. Least-Privilege Access**
- Users/workloads only get access they need for their role
- No carte blanche access to entire network
- Reduces blast radius of potential breaches

**2. Continuous Verification**
- Logins and connections time out periodically
- Forces users and devices to be continuously re-verified
- No "verify once, trust forever"

**3. Limiting Exposure**
- Outbound-only proxies
- No public IP addresses or ingress paths
- Connect via Zero Trust vendors
- Only proxy authenticated traffic

### ZTNA Principles (Zero Trust Network Access)

- **Application vs Network Access**: Connecting to network does NOT grant app access
- **Hidden IP Addresses**: Network invisible to connected devices except target service
- **Microsegmentation**: Minimize attack surface, prevent lateral movement

### Implementation
- Every request inspected, authenticated, encrypted, logged
- Continuous identity authentication proxy
- Physically separated from network perimeter
- Clear auditability
- Rapid access revocation

---

## SPIFFE: Workload Identity Framework

Source: [SPIFFE.io](https://spiffe.io/docs/latest/spiffe-about/overview/)

### What SPIFFE Solves
- Identity for workloads in dynamic environments
- Cryptographic identity credentials issued automatically
- Short-lived identity documents (SVIDs)
- No manual credential management

### SVID Types (SPIFFE Verifiable Identity Documents)

**1. X.509 SVIDs**
- Certificate-based identity
- For TLS connections (mTLS)

**2. JWT SVIDs**
- Token-based identity
- For API authentication

### Trust Establishment

**Node Attestation**
- Verify host identity
- Methods: cloud metadata, TPMs, join tokens
- Proves "this machine is who it claims to be"

**Workload Attestation**
- Verify runtime conditions
- Methods: K8s namespace, service account, container image, process metadata
- Proves "this process is authorized to receive identity"

### Edge Application
- Automatic credential rotation for long-lived devices
- Federation for multi-domain edge deployments
- Attestation-based verification of device authenticity
- Heterogeneous environment support

---

## SPIRE: SPIFFE Implementation

Source: [SPIFFE SPIRE Concepts](https://spiffe.io/docs/latest/spire-about/spire-concepts/)

### Architecture

**SPIRE Server**
- Central authority
- Issues SVIDs
- Manages trust
- High-availability deployment (etcd/PostgreSQL backend)

**SPIRE Agent**
- Runs on each node (DaemonSet in K8s)
- Performs node attestation
- Exposes Workload API
- Issues SVIDs to local workloads

### Node Attestation Methods
- AWS Instance Identity Document
- Azure VM metadata
- Google Compute Engine
- Kubernetes Service Account Tokens (PSAT)
- TPM-based attestation
- Join tokens (simplest, for bootstrap)

### Workload Attestation
- Kubernetes namespace, service account, labels
- Container image hash
- Process metadata (UID, GID, binary path)
- Custom selectors

### Deployment Pattern
```
SPIRE Server (cloud)
       │
       │ mTLS
       ▼
SPIRE Agent (edge node - DaemonSet)
       │
       │ Unix socket
       ▼
Workload (container/process)
```

---

## SPIFFE for Secretless Authentication

Source: [arXiv - SPIFFE-Based Authentication](https://arxiv.org/html/2504.14760v1)

### Secretless Patterns
- Replace long-lived credentials with ephemeral identity documents
- JWT-SVIDs issued at runtime prove workload identity
- Cloud providers validate tokens, grant scoped permissions
- No credential storage or rotation overhead

### Key Benefits

**1. Per-Job Identity Isolation**
- No shared credentials across workloads
- Each workload gets unique identity

**2. Time-Bounded Credentials**
- SVIDs include expiration
- Automatic rotation
- Reduced exposure window

**3. Verifiable Binding**
- Credentials bound to workload metadata
- Attestation proves legitimacy

**4. Portable Identities**
- Usable across cloud providers
- Works in hybrid environments

### Architecture Recommendation
- SPIRE server backed by etcd/PostgreSQL
- Agents as K8s DaemonSets or on CI runners
- Trust federation for multi-domain
- Layer policy engines (OPA/Cedar) above identity

---

## Federated Workload Authentication at Scale

Source: Research Paper Analysis

### Scale
- Tested across 100+ Kubernetes clusters
- Multiple public clouds

### Results
- Static keys replaced with on-demand tokens
- Token lifetime < 1 hour (vs permanent credentials)
- 99% reduction in average credential lifetime
- 80% reduction in compliance audit time

### Pattern
```
Workload → Attested Identity → Short-lived Token → Resource Access
```

---

## Device Attestation for Zero Trust

Source: [Device Authority](https://deviceauthority.com/device-identity-in-zero-trust-closing-the-security-gap/)

### Problem
- Nearly 1 in 5 organizations suffered security incidents related to non-human identities
- Only 15% confident in securing non-human identities

### Solution: Device Attestation
- Cryptographic verification starts at launch
- Platform-specific mechanisms:
  - AWS instance metadata
  - Kubernetes service account tokens
  - GitHub Actions OIDC tokens
- Identity survives through proxies, load balancers

### Secretless Architecture
- Eliminate hardcoded API keys, passwords, long-lived tokens
- Ephemeral credentials issued just-in-time
- Based on verified identity and current context
- Workloads never handle credentials directly

---

## Application to Harbor Satellite

### Current State (Problems)
- Static tokens for bootstrap
- Robot credentials stored plaintext
- No mTLS
- No credential rotation

### Target State (Zero-Trust)

**Bootstrap**
1. Join token for initial attestation
2. Device fingerprint as secondary factor
3. One-time use, immediately invalidated

**Ongoing Identity**
1. Short-lived SVIDs (X.509 certs)
2. Automatic rotation
3. mTLS for all communications
4. No stored credentials

**Attestation Layers**
1. Node: Device fingerprint, TPM, cloud metadata
2. Workload: Binary hash, process metadata
3. Continuous: Re-attestation on reconnection

---

## Implementation Strategy

### Phase 1: Lightweight Built-in
- Device fingerprint (MAC, CPU ID, etc.)
- Join token bootstrap
- Short-lived tokens with rotation
- Simpler than full SPIFFE/SPIRE

### Phase 2: SPIFFE/SPIRE Integration
- External SPIRE agent support
- Full attestation capabilities
- Federation for multi-domain
- Enterprise-grade identity

---

## Sources

- [Cloudflare Zero Trust](https://www.cloudflare.com/learning/security/glossary/what-is-zero-trust/)
- [SPIFFE Overview](https://spiffe.io/docs/latest/spiffe-about/overview/)
- [SPIRE Concepts](https://spiffe.io/docs/latest/spire-about/spire-concepts/)
- [SPIFFE-Based CI/CD Auth](https://arxiv.org/html/2504.14760v1)
- [Red Hat Zero Trust Workload Identity](https://www.redhat.com/en/blog/zero-trust-workload-identity-manager-now-available-tech-preview)
- [Device Authority](https://deviceauthority.com/device-identity-in-zero-trust-closing-the-security-gap/)
- [Security Boulevard - Identity Over Network](https://securityboulevard.com/2025/12/identity-over-network-why-2026-zero-trust-is-about-who-what-not-where/)
