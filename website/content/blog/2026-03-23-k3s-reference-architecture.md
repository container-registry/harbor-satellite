---
title: "K3s + Harbor Satellite at the Edge: Full Tutorial (Architecture + 2 Methods)"
date: 2026-03-23T10:00:00+01:00
author: aloui-ikram
description: "A complete step-by-step tutorial to run K3s with Harbor Satellite using Network Mirror or Automated Air-Gap Direct Delivery."
tags:
  - K3s
  - Harbor-Satellite
  - Edge-Computing
  - Spiffe
  - Spire
  - Zot
---

Deploying Kubernetes at the edge introduces architectural challenges not seen in centralized datacenters. Edge nodes often run with intermittent, low-bandwidth, or metered connectivity. At scale, relying on a centralized registry over WAN becomes a fragile single point of failure.

Harbor Satellite mitigates this by placing a lightweight local OCI registry at each edge site, powered by Zot. It synchronizes layers from Central Harbor when connectivity is available, then serves local K3s workloads without external dependency.

## 1. Introduction & Challenges Addressed

### Challenges & Solutions

| Edge challenge | Harbor Satellite solution |
|---|---|
| Workload failures during network partitions | Local Zot cache serves images over loopback; WAN status becomes non-blocking for runtime pulls. |
| High bandwidth costs on metered links | Layer-diff synchronization transfers only changed layers. |
| Bootstrapping restricted clusters | Direct Delivery injects images into K3s auto-import path. |
| Credential management at scale | SPIFFE/SPIRE Zero-Touch Registration (ZTR) removes static secrets from edge devices. |
| Certificate lifecycle overhead | SPIRE Workload API rotates X.509 SVIDs automatically. |

## 2. Reference Architecture

### 2.1 Network topology

- Cloud/datacenter plane: central registry, fleet management, and identity authority.
- Edge site plane: localized caching, workload runtime, and node attestation.

![Architecture Overview](/images/blog/architecture-overview.png)

### 2.2 Diagram workflow

| Flow | Description |
|---|---|
| Desired state | Satellite polls Ground Control for assignments, then pulls required OCI layers from Harbor over mTLS. |
| Reconciliation loop | Satellite compares local state to cloud state, pulls new layers, and prunes stale content. |
| Containerd mirroring | K3s `containerd` routes pulls to `127.0.0.1:5050` for local delivery. |
| Event streaming | Event Forwarder/Executor handles telemetry and execution command exchange. |

### 2.3 Component placement

| Component | Deployment location | Primary role |
|---|---|---|
| Central Harbor | Cloud | Source of truth for enterprise images |
| Ground Control | Cloud | Fleet orchestration and credential brokering |
| SPIRE Server | Cloud | Root identity authority |
| SPIRE Agent (GC) | Cloud | Issues SVIDs to Ground Control |
| SPIRE Agent (Edge) | Edge node | Attests node and issues SVIDs to Satellite |
| Harbor Satellite | Edge node | Local OCI cache and replication engine |
| K3s + containerd | Edge node | Runtime configured to use the local mirror |

## 3. Security Model: SPIFFE/SPIRE Integration

Static registry credentials at edge scale are high risk. This architecture uses SPIFFE/SPIRE cryptographic identity with Zero-Touch Registration instead of distributing long-lived secrets.

![SPIFFE Security Model](/images/blog/spiffe-security-model.png)

### 3.1 Zero-Touch Registration provisioning flow

1. Token generation: Ground Control registers a new Satellite and SPIRE issues a one-time join token.
2. Device attestation: Edge SPIRE Agent consumes the token and receives node identity.
3. Workload identity: Harbor Satellite requests an X.509 SVID from local SPIRE Agent.
4. Credential brokering: Satellite presents SVID to Ground Control over mTLS; Ground Control validates SPIFFE ID and returns scoped Harbor robot credentials.
5. Steady state: Satellite encrypts credentials with device-bound fingerprint material and continues autonomous sync.

### 3.2 Certificate rotation and device-bound encryption

- Automated rotation: short-lived SVIDs are renewed by SPIRE before expiration.
- Hardware-change protection: credentials are encrypted with a fingerprint derived from machine-id, MAC, and disk attributes.

### 3.3 SPIRE attestation methods

| Method | Best fit | Notes |
|---|---|---|
| `join-token` | Fast onboarding and test environments | One-time token, minimal prerequisites |
| `x509pop` | PKI-centric production environments | X.509 proof-of-possession attestation |
| `sshpop` | SSH CA-backed fleets | SSH host identity proof-of-possession |

### 3.4 Trust domain design

- Single trust domain: suitable when cloud and edge are managed by one platform/security team.
- Federated trust domains: suitable when regions or organizations require separate trust roots.

## 4. Connectivity Model

Harbor Satellite treats WAN as an optional optimization, not an operational requirement.

### 4.1 Background schedulers

| Scheduler | Interval | Behavior |
|---|---|---|
| State Replication | 10 seconds | Fetch desired state, pull missing layers, prune stale artifacts |
| Telemetry Heartbeat | 30 seconds | Report CPU/memory/disk and inventory status |
| Registration retry | 30 seconds | Re-authenticate and refresh credentials when required |

### 4.2 Bandwidth optimization with layer-diff

1. Fetch lightweight image manifests from Harbor.
2. Compare layer digests with local Zot cache.
3. Pull only missing or changed layers.

### 4.3 WAN outage behavior

When WAN is unavailable, replication and heartbeat enter retry mode. Local Zot on port `5050` remains operational, and K3s continues pulling images locally without disrupting workload startup.

---

## Method 1: Network-Based Registry Mirror

Use this when edge sites have intermittent but usable WAN.

### Prerequisites

- Linux edge node with K3s
- Docker + Docker Compose
- Central Harbor reachable at `http://<CENTRAL_HARBOR_IP>:80`

### Step 1: Seed image in Central Harbor

```bash
# Pull the standard Nginx image
docker pull nginx:alpine

# Tag it for your Central Harbor
docker tag nginx:alpine <CENTRAL_HARBOR_IP>:80/library/nginx:alpine

# Login and push the image to Central Harbor
docker login -u admin -p <HARBOR_PASSWORD> <CENTRAL_HARBOR_IP>:80
docker push <CENTRAL_HARBOR_IP>:80/library/nginx:alpine

# Remove local copies to ensure a clean test later
docker rmi nginx:alpine
docker rmi <CENTRAL_HARBOR_IP>:80/library/nginx:alpine
```

### Step 2: Configure K3s mirror to Satellite

```bash
sudo mkdir -p /etc/rancher/k3s
sudo cat <<EOF_K3S > /etc/rancher/k3s/registries.yaml
mirrors:
  "docker.io":
    endpoint:
      - "http://127.0.0.1:5050"
EOF_K3S
```

```bash
# Apply the new mirror settings
sudo systemctl restart k3s

# Force K3s to forget any previously cached images
sudo k3s crictl rmi --prune
```

Optional mirror wiring from Satellite runtime flags:

```bash
go run cmd/main.go \
  --token "<token>" \
  --ground-control-url "https://<GROUND_CONTROL_HOST>:9080" \
  --mirrors=containerd:docker.io
```

### Step 3: Start Ground Control and Satellite (Zero-Touch)

```bash
cd deploy/quickstart/spiffe/join-token/external/gc
HARBOR_URL=http://<CENTRAL_HARBOR_IP>:80 ./setup.sh
```

```bash
cd ../sat
./setup.sh
```

Verify SPIFFE onboarding and robot account provisioning:

```bash
docker logs ground-control | grep "SPIFFE ZTR"
```

### Step 4: Assign image sync policy to the edge

```bash
# Get Ground Control Bearer Token
TOKEN=$(curl -sk -X POST "https://localhost:9080/login" -d '{"username":"admin","password":"<HARBOR_PASSWORD>"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# Get the SHA256 Digest from Central Harbor
DIGEST=$(curl -sk -u "admin:<HARBOR_PASSWORD>" "http://<CENTRAL_HARBOR_IP>/api/v2.0/projects/library/repositories/nginx/artifacts?q=tags%3Dalpine&page_size=1" | grep -m1 '"digest":' | cut -d'"' -f4)
```

```bash
# Create the Edge Group
curl -sk -X POST "https://localhost:9080/api/groups/sync" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "{\"group\": \"edge-group\", \"registry\": \"http://<CENTRAL_HARBOR_IP>:80\", \"artifacts\": [{\"repository\": \"library/nginx\", \"tag\": [\"alpine\"], \"type\": \"image\", \"digest\": \"${DIGEST}\"}]}"

# Link the Satellite to the Group
curl -sk -X POST "https://localhost:9080/api/groups/satellite" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{"satellite": "edge-01", "group": "edge-group"}'
```

Verify artifact replication to local Satellite:

```bash
curl -s http://127.0.0.1:5050/v2/_catalog
# Expected: {"repositories":["library/nginx"]}
```

### Step 5: Air-gap behavior test

```bash
# Simulate outage of central components
docker stop harbor-core ground-control harbor-db redis registry registryctl harbor-portal harbor-log harbor-jobservice nginx
```

```bash
# Deploy workload using standard image name
kubectl run true-airgap-test --image=nginx:alpine
```

Validation checks:

```bash
kubectl describe pod true-airgap-test | grep -A 5 "Events:"
```

```bash
docker logs satellite | grep "nginx/blobs"
```

---

## Method 2: Automated Air-Gap via Direct Delivery

Use this when sites must run with no live registry path.

### Prerequisites

- Method 1 completed through sync assignment
- Root access on K3s node
- Edit access to `deploy/quickstart/spiffe/join-token/external/sat/docker-compose.yml`

### Step 1: Enable Direct Delivery in Satellite

```yaml
# deploy/quickstart/spiffe/join-token/external/sat/docker-compose.yml
services:
  satellite:
    environment:
      - DIRECT_DELIVERY=true
      - IMAGE_DIR=/var/lib/rancher/k3s/agent/images
    volumes:
      - /var/lib/rancher/k3s/agent/images:/var/lib/rancher/k3s/agent/images
```

Restart Satellite:

```bash
cd deploy/quickstart/spiffe/join-token/external/sat
docker compose up -d satellite --build

# Optional: confirm Direct Delivery is active
docker logs satellite | grep -E "direct delivery enabled|Direct delivery: tarball written"
```

If using RKE2, set `IMAGE_DIR=/var/lib/rancher/rke2/agent/images`.

### Step 2: Trigger sync and verify auto-import

```bash
# Get Ground Control Bearer Token
TOKEN=$(curl -sk -X POST "https://localhost:9080/login" -d '{"username":"admin","password":"<HARBOR_PASSWORD>"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# Get image digest from Harbor
DIGEST=$(curl -sk -u "admin:<HARBOR_PASSWORD>" "http://<CENTRAL_HARBOR_IP>/api/v2.0/projects/library/repositories/nginx/artifacts?q=tags%3Dalpine&page_size=1" | grep -m1 '"digest":' | cut -d'"' -f4)

# Create sync group
curl -sk -X POST "https://localhost:9080/api/groups/sync" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "{\"group\": \"edge-group\", \"registry\": \"http://<CENTRAL_HARBOR_IP>:80\", \"artifacts\": [{\"repository\": \"library/nginx\", \"tag\": [\"alpine\"], \"type\": \"image\", \"digest\": \"${DIGEST}\"}]}"

# Assign satellite to group
curl -sk -X POST "https://localhost:9080/api/groups/satellite" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{"satellite": "edge-01", "group": "edge-group"}'

# Confirm Satellite has the artifact
curl -s http://127.0.0.1:5050/v2/_catalog
```

Then verify image appears in K3s runtime:

```bash
sudo k3s crictl rmi --prune
sudo k3s crictl images | grep "<CENTRAL_HARBOR_IP>:80/library/nginx"
```

### Step 3: Offline validation

```bash
# Simulate outage
docker stop satellite spire-agent-satellite ground-control harbor-core harbor-db harbor-jobservice harbor-portal harbor-satellite-postgres harbor-log

# Deploy with upstream Harbor URL
sudo kubectl run test --image=<CENTRAL_HARBOR_IP>:80/library/nginx:alpine
sudo kubectl get pod test
```

Verify cached image hit:

```bash
sudo kubectl describe pod test | grep "Container image"
```

Expected signal: image is already present on machine.

---

## Choosing between methods

| Need | Recommended method |
|---|---|
| Simple operations and periodic connectivity | Method 1 (Network Mirror) |
| Maximum offline assurance | Method 2 (Direct Delivery) |

## Enterprise use cases

### Retail / POS

- Challenge: WAN outage at a branch blocks image pulls and POS restarts.
- Solution: Satellite keeps POS images local, so K3s workloads continue from `127.0.0.1:5050`.

### Industrial IoT (SUSE + Bosch model)

- Challenge: Factory edge sites operate on restricted networks and cannot tolerate restart failures.
- Solution: Ground Control pre-stages required artifacts to Satellite; K3s continues local pulls during WAN interruptions.

## Final note

Both methods follow the same architecture: centralized policy, edge-local artifacts, and identity-based trust. Start with Method 1 for faster rollout, then move critical sites to Method 2 when strict offline guarantees are required.

## References & Further Reading

- Zot project: https://zotregistry.dev/
- K3s private registry docs: https://docs.k3s.io/installation/private-registry
- SPIFFE/SPIRE docs: https://spiffe.io/docs/latest/spire-about/
- SUSE Edge docs: https://documentation.suse.com/suse-edge/
