# How Harbor Satellite Works

This document walks through the complete flow of Harbor Satellite - from deploying the cloud components to a satellite pulling images at the edge.

```
                     CLOUD                                        EDGE
  +----------+    +---------------+    +-------------+
  |  Harbor   |<-->| Ground Control|<-->| SPIRE Server| <--- (attestation) ---+
  | Registry  |    |               |    |             |                        |
  +----------+    +-------+-------+    +------+------+                        |
                          |                   |                                |
                          |            +------+------+                  +------+------+
                          |            | SPIRE Agent |                  | SPIRE Agent |
                          |            |    (GC)     |                  | (Satellite) |
                          |            +-------------+                  +------+------+
                          |                                                    |
                          +------------- mTLS (SVID) -------------------------+
                                                                               |
                                                                        +------+------+
                                                                        |  Satellite   |
                                                                        |   (Zot)      |
                                                                        +--------------+
```

## Key Terms

- **SPIFFE** - Secure Production Identity Framework for Everyone. An open standard for service identity.
- **SPIRE** - The SPIFFE Runtime Environment. The software that implements SPIFFE.
- **SVID** - SPIFFE Verifiable Identity Document. An X.509 certificate that contains a service's SPIFFE ID.
- **mTLS** - Mutual TLS. Both the client and server present certificates and verify each other's identity.
- **Trust Domain** - A SPIFFE trust boundary (e.g., `harbor-satellite.local`). All identities within a trust domain share the same root of trust.
- **Workload Entry** - A SPIRE registration that maps a workload to a SPIFFE ID using selectors (e.g., Docker labels, Kubernetes pod properties).
- **Selectors** - Attributes SPIRE uses to identify workloads (e.g., `docker:label:service:satellite`, `k8s:pod-label:app:satellite`).
- **Robot Account** - A Harbor service account with scoped pull/push permissions, used by satellites to access images.
- **OCI Artifact** - A generic blob stored in an OCI-compliant registry. Harbor Satellite uses OCI artifacts to store state and config alongside container images.
- **ZTR** - Zero-Touch Registration. The process by which a satellite registers with Ground Control without any pre-shared secrets.

## Components at a Glance

| Component | Where | Role |
|-----------|-------|------|
| Harbor | Cloud | Central container registry holding all images |
| Ground Control | Cloud | Fleet management, identity, credential rotation |
| SPIRE Server | Cloud | Issues and manages X.509 identities |
| SPIRE Agent (GC) | Cloud | Provides identity to Ground Control |
| SPIRE Agent (Satellite) | Edge | Provides identity to Satellite |
| Satellite | Edge | Local OCI registry (Zot) + image replication |

## The Complete Flow

### Phase 1: Cloud Deployment

Deploy these components in your cloud environment:

**Step 1 - Deploy Harbor**

Set up your Harbor registry with the images you want to distribute. For example, push `nginx:latest` and `alpine:latest` to a project in Harbor.

**Step 2 - Deploy SPIRE Server and Agent**

Deploy a SPIRE server and a SPIRE agent in the cloud. The SPIRE agent runs alongside Ground Control and provides it with a hardware-backed X.509 identity (SVID).

The SPIRE server configuration uses a trust domain (e.g., `harbor-satellite.local`) and supports multiple attestation methods:
- **Join Token** - One-time tokens for bootstrapping agents (simplest)
- **X.509 PoP** - Pre-provisioned certificates (production PKI)
- **SSH PoP** - SSH host certificates (existing SSH CA infrastructure)

**Step 3 - Deploy Ground Control**

Ground Control starts up, connects to the local SPIRE agent, and gets its own identity:
```
spiffe://harbor-satellite.local/gc/main
```

Ground Control also needs Harbor credentials (`HARBOR_USERNAME`, `HARBOR_PASSWORD`, `HARBOR_URL`) so it can:
- Create robot accounts for satellites
- Push state and config artifacts to Harbor
- Manage image group assignments

### Phase 2: Register a Satellite

With the cloud side running, register your first satellite through Ground Control's API.

**Step 4 - Create a Satellite**

Register a satellite in Ground Control. You provide:
- A name (e.g., `edge-us-east-01`)
- Optional region (e.g., `us-east`)
- SPIFFE selectors for identity verification (e.g., Docker labels, Kubernetes selectors, AWS instance IDs)

Ground Control:
1. Creates a SPIRE workload entry for the satellite with SPIFFE ID:
   ```
   spiffe://harbor-satellite.local/satellite/region/us-east/edge-us-east-01
   ```
2. Generates a join token for the satellite's SPIRE agent to bootstrap with
3. Creates a robot account in Harbor with pull permissions
4. Creates a default config for the satellite

**Step 5 - Create a Group and Assign Images**

Create a group (e.g., `us-east-group`) and add images to it:
- `library/nginx:latest`
- `library/alpine:latest`

Then assign this group to the satellite. A satellite can belong to multiple groups, and a group can be assigned to multiple satellites.

Ground Control stores the group state as an OCI artifact in Harbor at:
```
harbor.example.com/satellite/group-state/us-east-group/state:latest
```

The satellite's root state (which groups it belongs to and its config) is stored at:
```
harbor.example.com/satellite/satellite-state/edge-us-east-01/state:latest
```

### Phase 3: Edge Deployment

**Step 6 - Deploy SPIRE Agent at the Edge**

Deploy a SPIRE agent on the edge device. Configure it with:
- The SPIRE server address (TCP port 8081 must be reachable from the edge device)
- The join token generated during satellite registration

The join token is a one-time bootstrap credential. Once the SPIRE agent uses it to attest, the token becomes invalid. After attestation, the agent receives a proper certificate-based identity that automatically rotates. This is the only secret that needs to be transported to the edge, and it is single-use.

The agent connects to the SPIRE server, attests itself, and becomes ready to issue SVIDs to local workloads.

**Step 7 - Start the Satellite**

Run the satellite binary with just two pieces of information:
```bash
satellite --ground-control-url https://gc.example.com \
          --spiffe-enabled \
          --spiffe-endpoint-socket unix:///run/spire/sockets/agent.sock
```

No secrets. No credentials. No config files to manage.

### Phase 4: Zero-Trust Registration

When the satellite starts, it goes through Zero-Touch Registration (ZTR):

**Step 8 - Get Identity**

The satellite connects to the local SPIRE agent through the Workload API socket. The SPIRE agent issues an X.509 SVID containing the satellite's SPIFFE ID:
```
spiffe://harbor-satellite.local/satellite/region/us-east/edge-us-east-01
```

**Step 9 - Register with Ground Control**

The satellite creates an mTLS HTTP client using its SVID and sends a request to Ground Control:
```
GET https://gc.example.com/satellites/spiffe-ztr
```

Ground Control:
1. Extracts the SPIFFE ID from the mTLS client certificate
2. Parses the satellite name and region from the SPIFFE ID path
3. Looks up (or auto-registers) the satellite in its database
4. Creates or refreshes a robot account in Harbor
5. Returns a `StateConfig` containing:
   - The satellite's root state URL (pointing to Harbor)
   - Robot account credentials (username + password)
   - Harbor registry URL

The satellite encrypts this config with a device fingerprint (derived from machine-id, MAC address, and disk serial) and writes it to disk.

### Phase 5: Steady State

The satellite runs three concurrent schedulers:

**Registration Scheduler** (default: every 30s)
- Runs ZTR to obtain robot account credentials
- On each cycle, Ground Control refreshes the robot account secret
- Once initial registration succeeds and the satellite has valid credentials, the scheduler completes

**State Replication Scheduler** (default: every 10s)
1. Fetches the root satellite state artifact from Harbor (list of group URLs + config URL)
2. For each group, fetches the group state artifact (list of images)
3. Compares current state vs desired state
4. Deletes images that were removed from groups
5. Replicates new or changed images from Harbor to the local Zot registry
6. Fetches and applies config changes (replication intervals, Zot settings)

**Heartbeat Scheduler** (default: every 30s)
- Reports satellite status to Ground Control (CPU, memory, storage, cached images)
- Endpoint: `POST /satellites/sync`

## Zero-Trust Identity

Traditional approach:
```
Admin generates credentials --> copies to every edge device --> rotates manually
```

Harbor Satellite approach:
```
One-time join token --> SPIRE agent attests --> automatic SVID identity --> mTLS to Ground Control
```

The only secret transported to the edge is a one-time SPIRE join token used to bootstrap the SPIRE agent. Once used, the token is invalidated. After that, the satellite's identity comes from its SVID, which is automatically issued and rotated by SPIRE. No registry credentials, no config files, and no ongoing secret management.

Ground Control trusts the satellite because SPIRE vouches for it. Ground Control also has privileged access to the SPIRE server API, allowing it to create workload entries and generate join tokens for new satellites.

Robot account credentials (used to pull images from Harbor) are:
- Created automatically by Ground Control
- Delivered over the mTLS connection
- Encrypted at rest with the device fingerprint
- Rotated on every ZTR cycle

If the satellite's hardware changes (different machine), the encrypted config becomes unreadable and the satellite re-does ZTR with its new SVID.

## State Replication

State is stored as OCI artifacts in Harbor. There are three types:

**Satellite State** - `satellite/satellite-state/{name}/state:latest`
```json
{
  "states": [
    "harbor.example.com/satellite/group-state/us-east-group/state:latest"
  ],
  "config": "harbor.example.com/satellite/config-state/default/state:latest"
}
```

**Group State** - `satellite/group-state/{group}/state:latest`
```json
{
  "group": "us-east-group",
  "artifacts": [
    {
      "repository": "library/nginx",
      "tag": "latest",
      "digest": "sha256:abc123...",
      "type": "image"
    }
  ]
}
```

**Config State** - `satellite/config-state/{config}/state:latest`
```json
{
  "app_config": {
    "log_level": "info",
    "state_replication_interval": "@every 00h00m10s",
    "heartbeat_interval": "@every 00h00m30s",
    "local_registry": { "url": "http://127.0.0.1:8585" }
  },
  "zot_config": {
    "storage": { "rootDirectory": "./zot" },
    "http": { "address": "0.0.0.0", "port": "8585" }
  }
}
```

The satellite fetches these artifacts using `crane` (a Go library for interacting with OCI registries), authenticating with its robot account credentials.

## Image Replication

When the satellite detects new images in its desired state, it replicates them from Harbor to its local Zot registry using a lazy-loading strategy:

1. Fetch the image manifest from Harbor (metadata only, no layers downloaded yet)
2. Check if the image already exists in Zot with the same digest
3. If it exists, skip it (no work needed)
4. If not, count which layers are missing at the destination
5. Pull only the missing layers from Harbor
6. Push to local Zot

This approach minimizes bandwidth usage - if an image update only changes one layer, only that layer gets transferred.

### Offline Behavior

If the satellite cannot reach Harbor or Ground Control (network outage), it continues serving images from its local Zot registry. Workloads keep pulling from the local registry without interruption. The state replication scheduler logs errors and retries on the next interval. Once connectivity is restored, replication resumes from where it left off.

## Container Runtime Mirroring

Once images are in the local Zot registry, the satellite can configure local container runtimes to use it as a mirror. This means workloads (Kubernetes pods, Docker containers) automatically pull from the local registry first, falling back to the central registry only if needed.

Supported runtimes:
- **containerd** - Configures registry mirrors in `/etc/containerd/config.toml`
- **Docker** - Configures mirror in `/etc/docker/daemon.json` (docker.io only)
- **CRI-O** - Configures mirrors in `/etc/crio/crio.conf.d/`
- **Podman** - Configures mirrors in `/etc/containers/registries.conf`

Configure mirroring with the `--mirrors` flag:
```bash
satellite --mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io
```
