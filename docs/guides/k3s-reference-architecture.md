# K3s & Harbor Satellite

## End-to-End Reference Guide and Architecture

[![Harbor](https://img.shields.io/badge/Harbor-Satellite-blue?logo=harbor)](https://satellite.container-registry.com)
[![K3s](https://img.shields.io/badge/K3s-Edge-orange?logo=k3s)](https://k3s.io)
[![SUSE](https://img.shields.io/badge/SUSE-Open%20Source-73BA25?logo=suse)](https://github.com/suse)

*A reference architecture covering network topology, SPIFFE/SPIRE security, end-to-end setup procedures, enterprise use cases, and ecosystem alignment for deploying Harbor Satellite with K3s.*

---

## Table of Contents

| # | Section |
|---|---|
| 1 | [Introduction & Challenges Addressed](#1-introduction--challenges-addressed) |
| 2 | [Reference Architecture](#2-reference-architecture) |
| 3 | [Security Model : SPIFFE/SPIRE Integration](#3-security-model--spiffespire-integration) |
| 4 | [Connectivity Model](#4-connectivity-model) |
| 5 | [Setup Guide: Method 1 - Registry Mirror via BYO Registry](#5-setup-guide-method-1---registry-mirror-via-byo-registry) |
| 6 | [Setup Guide: Method 2 - Automated Air-Gap via Direct Delivery](#6-setup-guide-method-2---automated-air-gap-via-direct-delivery) |
| 7 | [Enterprise Use Cases](#7-enterprise-use-cases) |
| 8 | [Ecosystem Alignment](#8-ecosystem-alignment) |
| 9 | [References & Further Reading](#9-references--further-reading) |

---

## 1. Introduction & Challenges Addressed

Deploying Kubernetes at the edge introduces architectural challenges that are not present in centralized cloud datacenters. Edge nodes frequently operate in resource-constrained environments with intermittent, low-bandwidth, or highly metered network connections. When orchestrating K3s across many remote sites, relying on a centralized container registry over a Wide Area Network (WAN) introduces a single point of failure.

**Harbor Satellite**, an edge extension of the CNCF-graduated Harbor registry, pre-positions OCI content at each edge site. It synchronizes the artifacts assigned to it by Ground Control from a central Harbor registry into one of two stores:

- **Local OCI image layout** (default): an ORAS-backed OCI layout on disk (`<config-dir>/oci`). It does not listen on any port, so workloads cannot pull from it directly.
- **BYO registry** (`--byo-registry`): an external OCI registry you run next to the node (for example `registry:2`). Satellite copies images into it and container runtimes pull from it.

Workloads receive images in one of two ways today:

- **Method 1:** a containerd mirror pointing at a BYO registry ([Section 5](#5-setup-guide-method-1---registry-mirror-via-byo-registry)).
- **Method 2:** the experimental k3s/RKE2 **direct delivery** mode, which writes image tarballs into the K3s auto-import directory ([Section 6](#6-setup-guide-method-2---automated-air-gap-via-direct-delivery)).

A transparent proxy that serves workloads from Satellite itself is designed in [ADR-0009](../decisions/0009-transparent-oci-registry-proxy.md). It is not wired into the satellite binary yet.

### Challenges & Solutions

| Edge Challenge | Harbor Satellite Solution |
|---|---|
| **Content unavailable during network partitions** | The BYO registry or the K3s node image store keeps synchronized images available while the WAN is down. |
| **High bandwidth costs on metered links** | Content-addressed copying transfers only blobs that are missing at the destination. |
| **Bootstrapping restricted clusters** | Direct delivery writes images into K3s auto-import without manual tarball handling. |
| **Credential management at scale** | SPIFFE/SPIRE Zero-Touch Registration (ZTR) replaces the per-satellite registration token with an attested workload identity. Ground Control then issues a Harbor robot account per satellite. |
| **Certificate rotation** | The SPIRE Workload API rotates X.509 SVIDs automatically. |

---

## 2. Reference Architecture

### 2.1 Network Topology

The architecture is divided into two operational planes separated by a network boundary:

- **Cloud / Datacenter Plane:** central registry, fleet management, and identity authority.
- **Edge Site Plane:** Satellite, the local image store, the workload runtime, and the SPIRE agent.

```text
        CLOUD                              |                 EDGE
                                           |
  +-----------+   state artifacts,         |
  |  Harbor   |<-- robot basic auth -------+------------+
  +-----------+   (image pulls)            |            |
        ^                                  |      +-----------+     +------------------+
        | robot accounts,                  |      | Satellite |---->| BYO registry     |
        | state artifacts                  |      +-----------+     | or K3s image dir |
  +----------------+  ZTR, heartbeat       |        |   ^           +------------------+
  | Ground Control |<-- (mTLS with SVID) --+--------+   |                    ^
  +----------------+                       |            | SVID               | pull / import
        ^                                  |      +-------------+     +-------------+
        | SVID                             |      | SPIRE agent |     | K3s         |
  +--------------+  node attestation       |      +-------------+     | containerd  |
  | SPIRE server |<------------------------+-------------+            +-------------+
  +--------------+                         |
```

**Flows:**

| Flow | Description |
|---|---|
| **Registration (ZTR)** | Satellite calls Ground Control once at first start (mTLS with its SVID, or a registration token when SPIFFE is off) and receives Harbor robot credentials plus its state artifact URL. |
| **Desired State** | Satellite reads its state artifacts from Harbor and pulls the listed images from Harbor using the robot account (HTTP basic auth). mTLS is used only between Satellite and Ground Control. |
| **Reconciliation Loop** | Satellite compares the fetched state with its last applied state, copies new artifacts, and removes artifacts that are no longer assigned. |
| **Heartbeat** | Satellite sends a status report (CPU, memory, storage, cached images, sync timing) to `POST /satellites/sync`. The response can carry events for Satellite to act on. There is no command channel beyond this. |
| **Workload delivery** | K3s containerd pulls from the BYO registry through a `registries.yaml` mirror (Method 1), or imports tarballs written by direct delivery (Method 2). |

### 2.2 Component Placement

| Component | Deployment Location | Primary Role |
|---|---|---|
| **Central Harbor** | Cloud | Source of truth for container images and state artifacts. |
| **Ground Control** | Cloud | Fleet management, group and config orchestration, robot account brokering. |
| **SPIRE Server** | Cloud | X.509 identity authority and root of the trust domain. |
| **SPIRE Agent (GC)** | Cloud (co-located) | Attests Ground Control and issues its SVID. |
| **SPIRE Agent (Edge)**| Edge Node | Attests the edge machine and issues SVIDs to Harbor Satellite. |
| **Harbor Satellite** | Edge Node | State replication engine. Runs as a standalone binary or container. |
| **BYO registry** (Method 1) | Edge Node | OCI registry that Satellite fills and containerd pulls from. |
| **K3s + containerd** | Edge Node | Lightweight Kubernetes runtime. |

### 2.3 Image Synchronization Flow

```text
╔══════════════════════════════════════════════════════════╗
║  SYNC PHASE (WAN available)                              ║
╠══════════════════════════════════════════════════════════╣
║                                                          ║
║  Ground Control assigns images to an edge group          ║
║    └──► Satellite reads group state from Harbor          ║
║            └──► Satellite pulls images (robot account)   ║
║                  └──► Copies into BYO registry           ║
║                       and/or writes K3s tarballs         ║
║                                                          ║
╠══════════════════════════════════════════════════════════╣
║  EXECUTION PHASE (offline capable)                       ║
╠══════════════════════════════════════════════════════════╣
║                                                          ║
║  K3s containerd                                          ║
║    └──► Method 1: mirror to BYO registry on the node     ║
║    └──► Method 2: image already imported from tarball    ║
║            └──► Workload starts without WAN access       ║
║                                                          ║
╚══════════════════════════════════════════════════════════╝
```

---

## 3. Security Model : SPIFFE/SPIRE Integration

Distributing shared registry credentials to many edge devices means one compromised device exposes the whole fleet. With SPIFFE/SPIRE, each satellite proves its identity with an X.509 SVID and receives its **own** Harbor robot account from Ground Control.

### 3.1 Zero-Touch Registration (ZTR) Provisioning Flow

1. **Token Generation:** A system admin calls `POST /api/satellites/register` on Ground Control with `attestation_method: join_token`. Ground Control creates the satellite record, the SPIRE workload entry, and a one-time SPIRE join token.
2. **Device Attestation:** The SPIRE agent on the edge node consumes the join token and attests to the SPIRE server. x509pop and sshpop attestation are also supported (see [3.3](#33-spire-attestation-methods-for-k3s-edge-nodes)).
3. **Workload Identity:** Harbor Satellite connects to the local SPIRE agent over its Unix socket and receives an X.509 SVID.
4. **Credential Brokering:** Satellite calls `GET /satellites/spiffe-ztr` over mTLS. Ground Control verifies the SPIFFE ID and returns Harbor robot credentials and the satellite state URL.
5. **Steady State:** Satellite writes the credentials to `config.json` in its config directory. ZTR does not run again after it succeeds.

### 3.2 Certificate Rotation & Credential Storage

- **SVID rotation:** The SPIRE Workload API renews SVIDs before they expire without restarting Satellite.
- **Robot account lifetime:** Robot accounts expire after `ROBOT_DURATION_DAYS` (Ground Control env, default `30`). Ground Control flags soon-to-expire credentials in the heartbeat response with a `refresh_credentials` event, and Satellite then calls `POST /sat/refresh` on Ground Control. At the time of writing, Ground Control does not register a route for `/sat/refresh`, so this automatic refresh does not complete. Plan robot lifetimes and re-registration accordingly.
- **Config encryption is opt-in:** By default the robot credentials are stored in plaintext in `config.json`. With `app_config.encrypt_config: true`, Satellite encrypts the file with AES-256-GCM using a key derived from a device fingerprint (`/etc/machine-id`, MAC address, and disk serial). A copy of the encrypted file cannot be decrypted on other hardware. Builds with the `nospiffe` tag do not encrypt.

### 3.3 SPIRE Attestation Methods for K3s Edge Nodes

| Method | Best Fit | Notes |
| --- | --- | --- |
| **join-token** | Fast onboarding, test environments | One-time token flow with minimal prerequisites. |
| **x509pop** | Production PKI environments | Uses pre-provisioned X.509 certs for proof-of-possession attestation. |
| **sshpop** | SSH CA-backed environments | Uses host SSH identity for proof-of-possession attestation. |

### 3.4 Trust Domain Design

- **Single trust domain:** Suitable when cloud and edge are run by one platform/security team.
- **Federated trust domains:** Suitable when multiple organizations, regions, or teams need separate trust roots with controlled federation between them.

---

## 4. Connectivity Model

> **Architectural Principle:** WAN connectivity is needed to sync. It is not needed to start workloads whose images were already synced.

### 4.1 Background Schedulers

| Scheduler | Default Interval | Behavior |
| --- | --- | --- |
| **Registration (ZTR)** | 5 seconds | Retries until ZTR succeeds, then stops. Skipped on later starts once credentials are stored. |
| **State Replication** | 30 seconds | Fetches desired state, copies new artifacts, and removes artifacts that are no longer assigned. |
| **Heartbeat** | 30 seconds | Sends CPU, memory, storage, and cached image data to Ground Control. |

### 4.2 Bandwidth Optimization

1. Satellite resolves each artifact in Harbor and compares its digest with the copy at the destination.
2. If the digests match, the artifact is skipped.
3. Otherwise it copies the artifact graph, transferring only blobs the destination does not already have.

Direct delivery (Method 2) pulls each changed image from Harbor a second time to write the tarball. Take that into account on metered links.

### 4.3 Network Outage Behavior

During a WAN partition, the state replication and heartbeat schedulers fail and retry on their next interval. Content already in the BYO registry or imported into the K3s image store stays available to workloads.

---

## 5. Setup Guide: Method 1 - Registry Mirror via BYO Registry

In this method a plain OCI registry runs on the edge node. Satellite copies the assigned images into it (BYO registry mode) and K3s containerd uses it as a mirror for the central Harbor. Satellite and the edge SPIRE agent run as Docker containers from the SPIFFE join-token quickstart.

### Prerequisites

- A Linux machine (Edge Node) with **K3s** installed.
- A reachable **Central Harbor Registry** (v2.10+).
- **Docker** and **Docker Compose** on the machines that run Ground Control and Satellite.
- A clone of this repository. The commands below use `examples/deploy/spiffe/join-token/external/`.

---

### Step 1: Prepare the Central Harbor & Seed Image

1. **Install Harbor:** Ensure Central Harbor is running at `http://<CENTRAL_HARBOR_IP>:80`.
2. **Push a Test Image:**

```bash
docker pull nginx:alpine
docker tag nginx:alpine <CENTRAL_HARBOR_IP>:80/library/nginx:alpine

docker login -u admin -p Harbor12345 <CENTRAL_HARBOR_IP>:80
docker push <CENTRAL_HARBOR_IP>:80/library/nginx:alpine

# Remove local copies to ensure a clean test later
docker rmi nginx:alpine <CENTRAL_HARBOR_IP>:80/library/nginx:alpine
```

---

### Step 2: Start Ground Control (with external SPIRE)

```bash
cd examples/deploy/spiffe/join-token/external/gc
HARBOR_URL=http://<CENTRAL_HARBOR_IP>:80 ADMIN_PASSWORD='<ADMIN_PASSWORD>' ./setup.sh
```

The script starts PostgreSQL, the SPIRE server (host port `${SPIRE_HOST_PORT:-9081}`), the Ground Control SPIRE agent, and Ground Control (HTTPS on host port `${GC_HOST_PORT:-9080}`). If unset, the compose file defaults `HARBOR_URL` to `http://host.docker.internal:8080`, `HARBOR_USERNAME`/`HARBOR_PASSWORD` to `admin`/`Harbor12345`, and `ADMIN_PASSWORD` to `Harbor12345`. Wait until the Ground Control logs show that it is serving.

---

### Step 3: Configure Satellite for BYO Registry Mode

Satellite reads the Harbor address from `HARBOR_REGISTRY_URL` (default `http://host.docker.internal:8080` in the quickstart). Export it before running `setup.sh` in Step 4:

```bash
export HARBOR_REGISTRY_URL=http://<CENTRAL_HARBOR_IP>:80
```

Then edit `examples/deploy/spiffe/join-token/external/sat/docker-compose.yml` to enable BYO mode and add a registry service, published on the node's loopback interface. The satellite container itself exposes no port.

```yaml
services:
  edge-registry:
    image: registry:2
    container_name: edge-registry
    ports:
      - "127.0.0.1:5050:5000"
    volumes:
      - edge-registry-data:/var/lib/registry
    restart: unless-stopped
    networks:
      - harbor-satellite

  satellite:
    environment:
      - BYO_REGISTRY=true
      - REGISTRY_URL=http://edge-registry:5000
      # keep the existing entries (GROUND_CONTROL_URL, HARBOR_REGISTRY_URL, USE_UNSECURE, SPIFFE_*, CONFIG_DIR)
    depends_on:
      # add next to the existing spire-agent-satellite entry
      edge-registry:
        condition: service_started

volumes:
  edge-registry-data:
```

`USE_UNSECURE=true`, already set in the quickstart, makes Satellite use plain HTTP for both Harbor and the BYO registry. Satellite keeps the Harbor repository path at the destination, so `<CENTRAL_HARBOR_IP>:80/library/nginx:alpine` is stored as `library/nginx:alpine` in the edge registry.

---

### Step 4: Register and Start Satellite

```bash
cd ../sat
ADMIN_PASSWORD='<ADMIN_PASSWORD>' ./setup.sh
```

`setup.sh` logs in to Ground Control, registers satellite `edge-01` through `POST /api/satellites/register` (join token), starts the edge SPIRE agent, and starts Satellite.

**Verification:** Confirm that SPIFFE ZTR succeeded:

```bash
docker logs ground-control 2>&1 | grep "SPIFFE ZTR"
docker logs satellite 2>&1 | grep -i ztr
```

---

### Step 5: Configure the K3s Registry Mirror

Point K3s containerd at the edge registry for the Harbor host:

```bash
sudo mkdir -p /etc/rancher/k3s
sudo tee /etc/rancher/k3s/registries.yaml > /dev/null << 'EOF'
mirrors:
  "<CENTRAL_HARBOR_IP>:80":
    endpoint:
      - "http://127.0.0.1:5050"
EOF

sudo systemctl restart k3s
```

The satellite `--mirrors` flag writes `/etc/containerd/certs.d`. K3s uses its own containerd configuration generated from `registries.yaml`, so configure K3s with `registries.yaml` as shown here.

---

### Step 6: Sync Content to the Edge

**1. Log in and read the image digest:**

```bash
TOKEN=$(curl -sk -X POST "https://localhost:9080/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"<ADMIN_PASSWORD>"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

DIGEST=$(curl -s -u "admin:Harbor12345" "http://<CENTRAL_HARBOR_IP>/api/v2.0/projects/library/repositories/nginx/artifacts?q=tags%3Dalpine&page_size=1" | grep -m1 '"digest":' | cut -d'"' -f4)
```

**2. Create the group and assign the satellite:**

```bash
curl -sk -X POST "https://localhost:9080/api/groups/sync" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "{\"group\": \"edge-group\", \"artifacts\": [{\"repository\": \"library/nginx\", \"tag\": [\"alpine\"], \"type\": \"image\", \"digest\": \"${DIGEST}\"}]}"

curl -sk -X POST "https://localhost:9080/api/groups/satellite" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{"satellite": "edge-01", "group": "edge-group"}'
```

Ground Control always uses its own `HARBOR_URL` for group state, so the request does not need a `registry` field.

**3. Verify the copy:** After one or two replication intervals (30 s each by default), check the edge registry:

```bash
curl -s http://127.0.0.1:5050/v2/_catalog
# Expected: {"repositories":["library/nginx"]}
```

---

### Step 7: Air-Gap Verification Test

**1. Simulate a WAN outage:** Stop Harbor and Ground Control (container names depend on your Harbor installation):

```bash
docker stop ground-control harbor-core harbor-db registry harbor-portal harbor-jobservice nginx
```

**2. Deploy the pod** with the Harbor image reference:

```bash
sudo k3s crictl rmi <CENTRAL_HARBOR_IP>:80/library/nginx:alpine 2>/dev/null || true
sudo kubectl run airgap-test --image=<CENTRAL_HARBOR_IP>:80/library/nginx:alpine
sudo kubectl get pod airgap-test
```

**3. Confirm the image came from the edge registry:**

```bash
sudo kubectl describe pod airgap-test | grep -A 5 "Events:"
docker logs edge-registry 2>&1 | grep "library/nginx"
```

The pod events show `Pulling image` followed by `Successfully pulled`, and the `edge-registry` access log shows the manifest and blob `GET` requests from containerd.

---

## 6. Setup Guide: Method 2 - Automated Air-Gap via Direct Delivery

> **Experimental.** Direct delivery is flagged experimental in the satellite binary.

With direct delivery enabled, Satellite writes one image tarball per synced artifact into the K3s image directory. K3s imports tar archives from that directory automatically. Workloads keep using the original Harbor reference (`<CENTRAL_HARBOR_IP>:80/library/nginx:alpine`) and do not need a mirror.

### Architectural Concept: Automated Tarball Injection

- After each successful replication, Satellite pulls the changed images from Harbor and writes them as `.tar` files to the image directory. Tarballs for artifacts removed from the group are deleted.
- The image directory is auto-detected (`/var/lib/rancher/k3s/agent/images` or `/var/lib/rancher/rke2/agent/images`), or set with `--image-dir` / `IMAGE_DIR`. If neither is found, Satellite exits with an error.
- Satellite still replicates into its store (the local OCI layout, or a BYO registry if configured).

---

### Method 2 Prerequisites

- A Linux Edge node running **K3s**.
- Ground Control and Satellite from Method 1 Steps 1 to 4. BYO mode is not required.
- Root privileges on the K3s node.

---

### Step 1: Enable Direct Delivery in Satellite

Mount the host K3s image directory into the Satellite container and enable the feature:

```yaml
# examples/deploy/spiffe/join-token/external/sat/docker-compose.yml
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
cd examples/deploy/spiffe/join-token/external/sat
docker compose up -d satellite --build

docker logs satellite 2>&1 | grep -E "direct delivery enabled|Direct delivery: tarball written"
```

For RKE2, use `/var/lib/rancher/rke2/agent/images`.

---

### Step 2: Trigger Sync and Verify Auto-Import

If you have not created the group yet, run Method 1 [Step 6](#step-6-sync-content-to-the-edge) items 1 and 2.

Wait for one or two replication intervals, then check the tarball and the K3s image store:

```bash
sudo ls -l /var/lib/rancher/k3s/agent/images/
sudo k3s crictl images | grep "<CENTRAL_HARBOR_IP>:80/library/nginx"
```

---

### Step 3: Simulate Air-Gap and Deploy

```bash
# Simulate outage
docker stop satellite spire-agent-satellite ground-control harbor-core harbor-db harbor-jobservice harbor-portal harbor-satellite-postgres

# Deploy with the Harbor reference
sudo kubectl run test --image=<CENTRAL_HARBOR_IP>:80/library/nginx:alpine
sudo kubectl get pod test
```

*Expected result: the pod reaches `Running` with Satellite, Ground Control and Harbor stopped, because K3s already imported the image.*

### Verification Logging

```bash
sudo kubectl describe pod test | grep "Container image"
```

*Expected event: `Container image "<CENTRAL_HARBOR_IP>:80/library/nginx:alpine" already present on machine`.*

---

## 7. Enterprise Use Cases

### 7.1 Retail / Point-of-Sale (POS)

- **Challenge:** A WAN outage at a retail store prevents POS terminals from restarting, halting revenue.
- **Solution:** Satellites keep critical POS images on site, in a BYO registry mirror or pre-imported by direct delivery. Updates are staged per store group via Ground Control to avoid saturating the WAN.

### 7.2 Industrial IoT / Manufacturing (SUSE + Bosch IIoT)

SUSE and Bosch describe a hybrid cloud control and monitoring architecture for Industrial IoT (IIoT), where edge environments must remain secure and operational under constrained connectivity.

- **The Edge Workloads:** Manufacturing control, monitoring, and analytics workloads run on local edge Kubernetes nodes.
- **The Challenge:** Industrial sites often run on restricted networks and cannot afford downtime when WAN links degrade or fail.
- **The Solution:** Harbor Satellite synchronizes the required images to each site during connectivity windows. During outages, K3s pulls from the on-site BYO registry (Method 1), and for fully isolated nodes **Method 2 (Direct Delivery)** preloads images into K3s auto-import. *(Reference: [SUSE + Bosch Joint Architecture](https://www.suse.com/c/suse-and-bosch-pioneering-industrial-iot-with-a-hybrid-cloud-control-and-monitoring-architecture/))*

### 7.3 Remote Fleet Management (Energy/Telecom)

- **Challenge:** Remote SCADA systems operate over expensive, metered cellular links.
- **Solution:** Satellite copies only blobs that are missing at the destination. Heartbeats show each site's cached images in Ground Control before a cutover.

### 7.4 Smart Agriculture / Remote Monitoring

- **Challenge:** Agricultural IoT edge nodes running sensor processing or AI camera inference operate on metered, intermittent cellular or satellite links.
- **Solution:** Large inference images are synchronized during connectivity windows and preloaded into K3s with direct delivery for offline operation. When connectivity returns, only missing blobs are transferred. Enabling `encrypt_config` keeps the Harbor robot credentials encrypted at rest.

---

## 8. Ecosystem Alignment

Harbor Satellite provides a **registry layer** within the SUSE and CNCF edge ecosystems:

| Component | Integration Value |
| --- | ---  |
| **K3s** | Integrates via `registries.yaml` mirrors (BYO registry) or image auto-import (direct delivery); no CRDs or operators required. |
| **SUSE Edge 3.x Stack (SLE Micro + K3s + Rancher)** | Satellite provides image availability at the site while the SUSE stack handles OS, orchestration, and lifecycle. |
| **Rancher Fleet** | Fleet synchronizes GitOps manifests; Satellite makes sure the referenced images are present at the edge site before they run. |
| **ATIP (Adaptive Telecom Infrastructure Platform)** | Complements telecom edge platforms with local image availability under constrained WAN links. |
| **Akri** | Discovered-device workloads can use images that Satellite already placed on the site. |
| **Elemental** | Can provision nodes that run the SPIRE agent and Satellite. There is no built-in Elemental integration; registration still goes through the Ground Control API. |

---

## 9. References & Further Reading

### Harbor Satellite

- **[Harbor Satellite Official Documentation](https://satellite.container-registry.com/docs/)** : *Guides on architecture, deployment patterns, and Ground Control API usage.*
- **[Harbor Satellite GitHub Repository](https://github.com/container-registry/harbor-satellite)** : *Source code, issue tracking, and contribution guidelines.*
- **[ADR-0009: Transparent OCI Registry Proxy](../decisions/0009-transparent-oci-registry-proxy.md)** : *Target architecture for serving workloads from Satellite directly.*
- **[ORAS Go](https://oras.land/docs/client_libraries/go/)** : *The OCI content APIs used by Satellite's local image-layout store.*

### K3s & SUSE Edge Ecosystem

- **[K3s Private Registry Configuration](https://docs.k3s.io/installation/private-registry)**  : *Official K3s documentation for `registries.yaml` mirrors.*
- **[K3s Air-Gap Install](https://docs.k3s.io/installation/airgap)** : *Documents the `agent/images` auto-import directory used by direct delivery.*
- **[SUSE + Bosch IIoT Architecture](https://www.suse.com/c/suse-and-bosch-pioneering-industrial-iot-with-a-hybrid-cloud-control-and-monitoring-architecture/)** : *Enterprise case study with K3s running workloads on restricted factory floors.*
- **[SUSE Edge Framework](https://documentation.suse.com/suse-edge/3.4/single-html/edge/edge.html)** : *Integrating SLE Micro, K3s, and GitOps at the edge.*
- **[Rancher Fleet Overview](https://ranchermanager.docs.rancher.com/v2.10/integrations-in-rancher/fleet/overview)** : *Multi-cluster GitOps operations.*
- **[SUSE ATIP Overview](https://documentation.suse.com/suse-edge/3.1/html/edge/atip.html)** : *Adaptive Telecom Infrastructure Platform.*
- **[SUSE Edge Akri Component](https://documentation.suse.com/en-us/suse-edge/3.1/html/edge/components-akri.html)** : *Akri in edge environments.*
- **[SUSE Edge Elemental Component](https://documentation.suse.com/suse-edge/3.5/html/edge/components-elemental.html)** : *Elemental node onboarding and lifecycle.*

### Security & Identity

- **[SPIFFE & SPIRE Architecture](https://spiffe.io/docs/latest/spire-about/)** : *How SPIFFE identities and SPIRE workload attestation work.*
- **[Harbor Satellite SPIFFE Quickstarts](../../examples/deploy/spiffe/README.md)** : *Join token, x509pop, and sshpop setup variants.*

---
