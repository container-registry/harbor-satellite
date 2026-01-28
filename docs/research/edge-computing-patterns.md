# Edge Computing Patterns Research

Research notes on how enterprises deploy and manage edge computing infrastructure.

## Chick-fil-A: The Gold Standard

Source: [DoK Community - Persistence at the Edge](https://dok.community/blog/persistence-at-the-edge/)

### Scale
- ~3,000 restaurants
- 3 Intel NUCs per location
- K3s on Ubuntu

### Key Architectural Decisions

**1. Outbound-Only Connections**
- Never inbound connections
- No VPNs required
- Edge nodes initiate all communication
- Works behind any firewall/NAT

**2. OAuth-Based Authentication**
- Custom OAuth server for edge nodes
- JWTs with embedded permissions
- No static credentials at edge

**3. Recoverable, Not HA**
- No persistence SLAs
- Databases are tools, not repositories
- Apps sync critical state to AWS
- On restart, rehydrate from cloud backups

**4. Partition-Based Provisioning**
- Remote device resets without physical access
- Essential for managing thousands of nodes

### Quote
> "we didn't do this 'cause we think it's cool"

Technology serves operational problems, not architectural elegance.

---

## Agent-Based Pull Model

Source: [Plural - Edge Kubernetes Deployment Guide](https://www.plural.sh/blog/edge-kubernetes-deployment-guide/)

### Pattern
- Lightweight agents on edge nodes
- Periodically fetch desired state from Git
- Handles intermittent connectivity gracefully
- Nodes retry when reconnected

### Node Autonomy
- Edge devices function independently during network outages
- Keeping workloads running without constant control plane contact

### Key Requirements

1. **Local Image Caching**
   - Nodes pre-cache container images locally
   - Pods can restart without downloading during connectivity windows

2. **Egress-Only Communication**
   - Eliminates inbound firewall exposure
   - Agents initiate outbound connections exclusively
   - Dramatically reduces attack surface

3. **GitOps-Driven Workflows**
   - Git serves as single source of truth
   - Infrastructure-as-Code templates
   - Self-service provisioning at scale

---

## Docker vs Kubernetes for Edge

Source: [Acumera - Why Docker Over Kubernetes](https://www.acumera.com/blog/why-i-advise-our-retail-edge-customers-to-choose-docker-over-kubernetes/)

### When K8s is Overkill

Kubernetes was designed for stable data centers with dedicated teams. Real retail edges face:
- Limited hardware footprints
- Intermittent connectivity
- No on-site IT support
- PCI DSS compliance demands
- Mission-critical payment systems

### When K8s Makes Sense
- Industrial IoT deployments
- Heavy AI inference requirements
- High-compute environments (energy, telecom, transportation)

### Core Philosophy
> "What is the right architecture for YOUR edge workloads?"

Don't force cloud-native tools onto edge constraints.

### Harbor Satellite Implication
Must work equally well with plain Docker, K3s, or full K8s.

---

## Air-Gapped Deployments

Source: [K3s Air-Gapped Setup](https://mpolinowski.github.io/docs/DevOps/Kubernetes/2022-11-19--k3s-air-gapped-kubernetes/2022-11-19/)

### Image Handling
- Pre-download images to `/var/lib/rancher/k3s/agent/images/`
- Use `INSTALL_K3S_SKIP_DOWNLOAD=true` flag

### Approaches
1. **Private Registry**: Deploy registry to mirror Docker Hub
2. **Manual Distribution**: For small clusters, more practical than registry

### Harbor Satellite Role
Be the local image cache that enables air-gapped operation.

---

## KubeEdge Architecture

Source: [CNCF - KubeEdge](https://www.cncf.io/blog/2022/08/18/kubernetes-on-the-edge-getting-started-with-kubeedge-and-kubernetes-for-edge-computing/)

### Components

**Cloud Side**
- CloudHub (websocket connections)
- EdgeController (node/pod metadata)
- DeviceController (device data sync)

**Edge Side**
- EdgeCore containing:
  - EdgeHub
  - Edged (container management)
  - MetaManager (SQLite persistence)
  - EventBus (MQTT client)
  - ServiceBus (HTTP client)
  - DeviceTwin (device status)
  - Mappers (IoT protocols)

### Key Capabilities
- 70MB memory requirement
- Scales to 100,000 concurrent edge nodes
- 6ms response times with 5% packet loss
- Offline operation via SQLite persistence
- Supports non-TCP/IP protocols (Modbus, OPC-UA, Bluetooth)

---

## Edge Architecture Requirements Summary

### Network
- Outbound-only connections
- No VPNs
- Works behind NAT/firewalls
- Egress-only for security

### Identity
- OAuth/JWT based
- No static credentials
- Short-lived tokens
- Auto-rotation

### Resilience
- Offline operation
- Local state persistence
- Automatic sync on reconnection
- Recoverable > HA

### Management
- Remote provisioning
- Fleet-wide operations
- No physical access required
- GitOps workflows

---

## Sources

- [Chick-fil-A Edge Computing](https://dok.community/blog/persistence-at-the-edge/)
- [Plural Edge K8s Guide](https://www.plural.sh/blog/edge-kubernetes-deployment-guide/)
- [Docker vs K8s for Edge](https://www.acumera.com/blog/why-i-advise-our-retail-edge-customers-to-choose-docker-over-kubernetes/)
- [K3s Air-Gapped](https://mpolinowski.github.io/docs/DevOps/Kubernetes/2022-11-19--k3s-air-gapped-kubernetes/2022-11-19/)
- [KubeEdge CNCF](https://www.cncf.io/blog/2022/08/18/kubernetes-on-the-edge-getting-started-with-kubeedge-and-kubernetes-for-edge-computing/)
- [Platform9 Edge Challenges](https://platform9.com/blog/edge-computing-challenges-and-opportunities/)
- [TrueFullstaq Edge Architecture](https://www.truefullstaq.com/en/blog/kubernetes-at-the-edge-architecture)
