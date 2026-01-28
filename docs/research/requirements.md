# Harbor Satellite Technical Requirements

Synthesized requirements for Harbor Satellite as the universal edge image delivery system.

## Vision

Harbor Satellite is THE glue between edge and cloud for container images.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          DEPLOYMENT TOOLS                                │
│   ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌────────────┐   │
│   │ Ansible │  │ FluxCD  │  │ ArgoCD  │  │Terraform│  │ Plain Bash │   │
│   └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘  └─────┬──────┘   │
│        │            │            │            │              │          │
│        └────────────┴─────┬──────┴────────────┴──────────────┘          │
│                           │                                              │
│                           ▼                                              │
│              ┌───────────────────────────┐                               │
│              │    HARBOR SATELLITE       │                               │
│              │  - Single binary          │                               │
│              │  - Zero-trust by default  │                               │
│              │  - Proxy cache            │                               │
│              │  - Offline resilient      │                               │
│              └─────────────┬─────────────┘                               │
│                            │                                             │
│        ┌───────────────────┼───────────────────┐                         │
│        │                   │                   │                         │
│        ▼                   ▼                   ▼                         │
│   ┌─────────┐        ┌──────────┐        ┌──────────┐                   │
│   │ Docker  │        │   K3s    │        │Raspberry │                   │
│   │ Compose │        │ MicroK8s │        │   Pi     │                   │
│   └─────────┘        └──────────┘        └──────────┘                   │
│                                                                          │
│   POS Systems    Industrial IoT    Retail Kiosks    Home Labs           │
└─────────────────────────────────────────────────────────────────────────┘
```

## Problems Solved

1. **Static secrets** - Every edge system has hardcoded credentials
2. **Image availability** - Edge needs images even when disconnected
3. **Complexity** - Current solutions require K8s expertise
4. **Universality** - One solution for ANY edge platform

---

## Core Requirements

### REQ-1: Universal Deployment Target

Harbor Satellite MUST work on:
- Kubernetes (full, K3s, MicroK8s, KubeEdge)
- Docker / Docker Compose
- Podman
- Bare metal / VMs
- Raspberry Pi / ARM devices
- POS systems
- Any Linux with container runtime

**Single binary, no dependencies beyond a container runtime.**

Priority: Docker/Compose first (simplicity over K8s-native features)

### REQ-2: Outbound-Only Communication

Following Chick-fil-A and zero-trust patterns:
- Satellite initiates ALL connections
- No inbound ports required
- Works behind NAT, firewalls, proxies
- No VPN required

### REQ-3: Offline Resilience

- Satellite MUST continue serving images when disconnected
- Local registry (Zot) persists images
- State sync resumes automatically on reconnection
- Graceful degradation (serve stale state vs fail)

### REQ-4: Secretless Operation

- No static credentials stored at edge
- No plaintext passwords in config files
- Short-lived, auto-rotating credentials only
- Credentials derived from workload identity

---

## Identity & Authentication Requirements

### REQ-5: Phased Identity Implementation

**Phase 1: Lightweight Built-in**
- Device fingerprint (MAC, CPU ID, boot ID)
- Join token bootstrap
- Short-lived tokens with automatic rotation
- Smaller binary, works everywhere

**Phase 2: SPIFFE/SPIRE Integration (Optional)**
- External SPIRE agent support
- Full attestation capabilities
- Enterprise-grade identity

### REQ-6: Certificate-Based Auth

- mTLS for Satellite <-> Ground Control
- mTLS for Satellite <-> Harbor
- Certificate pinning for known endpoints
- Automatic certificate rotation

### REQ-7: Bootstrap Trust

Multiple bootstrap methods:
1. Join token (simplest, for initial deployment)
2. TPM attestation (for trusted hardware)
3. Cloud provider attestation (AWS, GCP, Azure metadata)
4. Kubernetes service account token attestation

---

## Security Requirements

### REQ-8: Zero-Trust Network Model

- Assume network is hostile
- Verify every request (continuous validation)
- Least-privilege access per satellite
- No implicit trust based on network location

### REQ-9: Blast Radius Containment

- Compromised satellite cannot access other satellites' state
- Per-satellite identity and permissions
- State artifacts scoped to individual satellite

### REQ-10: Audit Trail

- Log all registration events
- Log all state sync operations
- Log credential issuance/rotation
- Forward logs to central system (optional)

---

## Operational Requirements

### REQ-11: Zero-Touch Provisioning

- Satellite auto-registers on first boot
- No manual configuration beyond initial token
- Self-configures based on Ground Control policies

### REQ-12: Fleet Management

- Ground Control manages satellite fleet
- Group-based policy assignment
- Bulk operations (update, rollback, decommission)

### REQ-13: Observability

- Health endpoints
- Metrics (Prometheus compatible)
- Distributed tracing (optional)
- Status reporting to Ground Control

---

## Compatibility Requirements

### REQ-14: Container Runtime Support

Support mirror configuration for:
- Docker (daemon.json)
- containerd (hosts.toml)
- CRI-O (registries.conf)
- Podman (registries.conf)

### REQ-15: Registry Compatibility

- OCI-compliant image distribution
- Harbor as upstream source of truth
- Zot as embedded local registry
- Optional: Spegel for P2P distribution

---

## Design Decisions (Confirmed)

### Ground Control Required
Harbor Satellite always requires Ground Control - no standalone proxy cache mode. This simplifies architecture and ensures consistent fleet management.

### Docker/Compose First
Prioritize simplicity and universal deployment. Single binary that "just works" on Docker, Compose, VMs, Raspberry Pi. K8s support is one deployment option, not the primary target.

### Identity Strategy
Phased approach:
- Phase 1: Lightweight built-in (smaller binary, works everywhere)
- Phase 2: SPIFFE/SPIRE integration as optional advanced mode

---

## Implementation Phases

### Phase 1: Foundation (Simplicity + Security)

Priority: Docker/Compose first, single binary

- [ ] Encrypt credentials at rest (sealed config using derived keys)
- [ ] Add TLS support for Ground Control communication
- [ ] Lightweight built-in attestation (join token + device fingerprint)
- [ ] Proxy cache mode with pull-through caching
- [ ] Simplified installation: `curl | sh` or single binary download
- [ ] Docker Compose example as primary deployment method

### Phase 2: Zero-Trust Identity (Built-in)

- [ ] Device identity based on hardware fingerprint
- [ ] Short-lived tokens with automatic rotation
- [ ] mTLS for all communications
- [ ] No plaintext credentials anywhere
- [ ] Audit logging for all operations

### Phase 3: SPIFFE/SPIRE Integration (Optional)

- [ ] External SPIRE agent support
- [ ] Kubernetes PSAT attestation
- [ ] Cloud provider attestation (AWS, GCP, Azure)
- [ ] TPM-based attestation for trusted hardware

### Phase 4: Fleet & Ecosystem

- [ ] Helm chart for K8s deployments
- [ ] Ansible playbook
- [ ] FluxCD/ArgoCD examples
- [ ] Terraform provider
- [ ] Push-based config updates
- [ ] Bulk satellite management
- [ ] Policy-based artifact distribution

---

## Verification Criteria

### Phase 1 Complete When:
1. Satellite installs via single command
2. Credentials encrypted in config file
3. TLS enforced for all connections
4. Works on Docker, Compose, Raspberry Pi

### Phase 2 Complete When:
1. No plaintext credentials anywhere
2. mTLS between all components
3. Automatic credential rotation
4. Audit logs capture all operations

### Phase 3 Complete When:
1. SPIRE agent integration working
2. K8s PSAT attestation verified
3. Cloud attestation (at least one provider)

### Phase 4 Complete When:
1. Deployment via Helm, Ansible, FluxCD works
2. Fleet operations from Ground Control
3. Policy-based distribution verified

---

## Non-Requirements (Out of Scope)

- Standalone mode without Ground Control
- Custom Harbor UI integrations
- Multi-tenancy within single satellite
- Image signing/verification (Harbor's responsibility)
- Full K8s operator (keep simple)

---

## Success Metrics

### Adoption
- Works on 90%+ of edge platforms without modification
- Installation time < 5 minutes
- No K8s expertise required

### Security
- Zero plaintext credentials in deployment
- Automatic credential rotation
- Blast radius limited to single satellite

### Reliability
- Offline operation for 30+ days
- Automatic recovery on reconnection
- < 1 minute state sync latency

---

## Related Documents

- [Edge Computing Patterns](./edge-computing-patterns.md)
- [Zero-Trust Identity](./zero-trust-identity.md)
- [Secrets Management](./secrets-management-edge.md)
- [Current Architecture Gaps](./current-architecture-gaps.md)
