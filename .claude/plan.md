# SPIFFE/SPIRE Integration Plan for Harbor Satellite

## Summary

Integrate SPIFFE/SPIRE for authenticating Satellites with Ground Control, replacing the current token-based ZTR mechanism with cryptographic workload identity using X.509 SVIDs and mTLS.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| SPIRE Deployment | Sidecar (default) + External (optional) | Simplicity for dev/testing, extensible for enterprise |
| Node Attestation | Join Token + TPM + x509pop (Phase 1) | Universal + hardware-backed + lazy mode; Cloud/K8s as TODO |
| Satellite Binary | Optional embedded SPIRE agent | Build-time flag for standalone vs. external SPIRE |
| Token Auth | Keep as fallback | Gradual migration, air-gapped support |
| Federation | Plan from start | Design SPIFFE ID scheme for multi-cluster |

---

## Background: Current Authentication

**Current Flow:**
1. Admin registers satellite via `POST /satellites` -> receives 24-hour single-use token
2. Satellite calls `GET /satellites/ztr/{token}` -> receives Harbor robot credentials
3. Token deleted after use; credentials stored locally
4. Satellite uses robot credentials to pull state artifacts from Harbor

**Limitations:**
- Token is single-use with 24-hour expiry (requires re-registration if missed)
- Credentials transmitted over wire (even with TLS, stored in response body)
- No workload attestation (any process with token can register)
- Manual token distribution required

---

## SPIFFE/SPIRE Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Ground Control (Cloud)                    │
│  ┌─────────────────┐        ┌─────────────────────────────┐ │
│  │  SPIRE Server   │◄──────►│  Ground Control Service     │ │
│  │  (Trust Domain) │        │  - Registration API         │ │
│  └────────┬────────┘        │  - State Management         │ │
│           │                 │  - Authorization (OPA)      │ │
│           │Federation       └─────────────────────────────┘ │
└───────────┼─────────────────────────────────────────────────┘
            │
            │ Trust Bundle Exchange
            │
┌───────────┼─────────────────────────────────────────────────┐
│           ▼              Edge Node                          │
│  ┌─────────────────┐        ┌─────────────────────────────┐ │
│  │  SPIRE Agent    │◄──────►│  Satellite Service          │ │
│  │  (Workload API) │  SVID  │  - State Replication        │ │
│  └─────────────────┘        │  - Local Registry (Zot)     │ │
│                             └─────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

---

## Key SPIFFE Concepts for This Integration

### SPIFFE ID Scheme (Federation-Ready)
```
Trust Domain Pattern: {org}.{env}.harbor-satellite
Example: acme.prod.harbor-satellite

# Ground Control
spiffe://acme.prod.harbor-satellite/gc/main

# Satellites by region/site
spiffe://acme.prod.harbor-satellite/satellite/region/us-west/edge-site-1
spiffe://acme.prod.harbor-satellite/satellite/region/eu-central/edge-site-2

# Federated trust domains (future)
spiffe://acme.staging.harbor-satellite/...  # Staging environment
spiffe://partner.prod.harbor-satellite/...   # Partner organization
```

This hierarchical scheme enables:
- Region-based authorization rules
- Environment isolation
- Future federation between trust domains

### Authentication Flow with SPIFFE
1. Satellite obtains X.509-SVID from local SPIRE Agent
2. Satellite presents SVID during mTLS handshake with Ground Control
3. Ground Control verifies SVID against trust bundle
4. Ground Control extracts SPIFFE ID to identify satellite
5. Authorization policy determines access (OPA recommended)

### Components
- **SPIRE Server**: Runs alongside Ground Control, issues SVIDs
- **SPIRE Agent**: Runs on edge nodes, provides Workload API
- **go-spiffe library**: Used by Satellite and Ground Control for mTLS
- **Trust Bundle**: Shared CA certificates for verification

---

## Implementation Phases

### Phase 1: SPIRE Infrastructure + Join Token + TPM Attestation

**Ground Control Side:**
1. Add SPIRE Server as optional sidecar (Docker Compose / K8s manifests)
2. Create SPIRE provider interface for extensibility:
   - `SPIREProvider` interface with `GetX509Source()`, `GetTrustBundle()`
   - `SidecarProvider` - connects to local SPIRE agent socket
   - `ExternalProvider` - connects to external SPIRE deployment
3. Configure node attestors (Phase 1):
   - `join_token` - simple bootstrap, works everywhere
   - `tpm_devid` - hardware-backed attestation for bare metal/edge devices
   - `x509pop` - X.509 proof of possession (lazy mode: single cert for all nodes)
4. Configure datastore (SQLite for dev, PostgreSQL for prod)

**Satellite Side:**
1. **Embedded SPIRE Agent** (build-time option):
   - Satellite binary optionally embeds SPIRE agent functionality
   - Single binary deployment for edge devices
   - Build flag: `go build -tags embedded_spire`
2. **External SPIRE Agent** (alternative):
   - Satellite connects to external SPIRE agent socket
   - For enterprise deployments with existing SPIRE infrastructure
3. Implement Workload API client using go-spiffe
4. Bootstrap with join token OR TPM attestation

**Build Configuration:**
```bash
# Build satellite with embedded SPIRE agent (standalone mode)
go build -tags embedded_spire -o satellite-standalone ./cmd

# Build satellite without SPIRE (requires external agent)
go build -o satellite ./cmd

# Dagger build variants
dagger call build --component=satellite --variant=standalone  # with embedded SPIRE
dagger call build --component=satellite --variant=minimal     # without SPIRE
```

**TPM Attestation (Phase 1 - High Security):**
```bash
# SPIRE Agent config for TPM-based attestation
NodeAttestor "tpm_devid" {
    plugin_data {
        devid_cert_path = "/etc/spire/devid.crt"
        devid_priv_path = "/etc/spire/devid.key"
    }
}

# Registration entry using TPM selectors (per-device identity)
spire-server entry create \
  -spiffeID spiffe://acme.prod.harbor-satellite/satellite/region/factory-floor/edge-1 \
  -parentID spiffe://acme.prod.harbor-satellite/spire-agent \
  -selector tpm_devid:subject:cn:edge-device-001
```

**X.509 PoP Attestation (Phase 1 - Lazy/Simple Mode):**
```bash
# Single certificate shared across all satellite nodes
# Good for: dev/test, small deployments, quick setup

# SPIRE Agent config for X.509 PoP attestation
NodeAttestor "x509pop" {
    plugin_data {
        private_key_path = "/etc/spire/satellite-fleet.key"
        certificate_path = "/etc/spire/satellite-fleet.crt"
    }
}

# Single registration entry matches ALL nodes with this cert
spire-server entry create \
  -spiffeID spiffe://acme.prod.harbor-satellite/satellite/fleet/all \
  -parentID spiffe://acme.prod.harbor-satellite/spire-agent \
  -selector x509pop:subject:cn:satellite-fleet

# Workload entry inherits from fleet parent
spire-server entry create \
  -spiffeID spiffe://acme.prod.harbor-satellite/satellite/region/us-west/${HOSTNAME} \
  -parentID spiffe://acme.prod.harbor-satellite/satellite/fleet/all \
  -selector unix:uid:1000
```

**Security Trade-offs:**
| Mode | Security | Convenience | Use Case |
|------|----------|-------------|----------|
| TPM | High | Low | Production, regulated environments |
| x509pop (per-node) | Medium | Medium | Production, non-regulated |
| x509pop (shared) | Low | High | Dev/test, small deployments |
| join_token | Medium | Medium | Initial bootstrap, migration |

**TODO (Future Phases) - Cloud/K8s Attestation:**

| Attestor | Environment | How It Works |
|----------|-------------|--------------|
| k8s_psat | Kubernetes | Uses K8s service account token; cluster vouches for pod identity |
| aws_iid | AWS EC2 | Uses EC2 Instance Identity Document; AWS vouches for instance |
| gcp_iit | GCP | Uses GCP Instance Identity Token; Google vouches for VM |
| azure_msi | Azure | Uses Managed Service Identity; Azure vouches for VM |

Note: Phase 1 includes `join_token`, `tpm_devid`, and `x509pop` attestors.

**Benefits of platform attestation**:
- No pre-shared secrets to distribute
- Automatic trust establishment based on where workload runs
- Tamper-resistant (cloud/hardware vouches for identity)

**Example: AWS EC2 satellite onboarding without join token**:
```bash
# SPIRE Agent config on EC2 instance
NodeAttestor "aws_iid" {
    plugin_data {
        # No token needed - uses EC2 metadata service
    }
}

# Registration entry on SPIRE Server (pre-created)
spire-server entry create \
  -spiffeID spiffe://acme.prod.harbor-satellite/satellite/region/us-west/edge-1 \
  -parentID spiffe://acme.prod.harbor-satellite/spire-agent \
  -selector aws_iid:tag:satellite-name:edge-1 \
  -selector aws_iid:region:us-west-2
```

The satellite agent starts, proves its EC2 identity to SPIRE Server, and gets SVID automatically.

### Phase 2: go-spiffe Integration

**Files to Modify (Ground Control):**
- `ground-control/internal/server/server.go` - Add SPIFFE mTLS
- `ground-control/internal/server/satellite_handlers.go` - Extract SPIFFE ID
- New: `ground-control/internal/spiffe/source.go` - X509Source wrapper

**Files to Modify (Satellite):**
- `internal/state/registration_process.go` - Use SPIFFE for auth
- `internal/tls/config.go` - Add SPIFFE TLS config
- New: `internal/spiffe/client.go` - Workload API client

### Phase 3: Registration Flow Changes

**New ZTR Flow:**
1. Admin creates satellite registration entry in SPIRE Server
2. Satellite obtains SVID from local SPIRE Agent
3. Satellite calls Ground Control with mTLS (SVID as client cert)
4. Ground Control extracts SPIFFE ID from cert SAN
5. Ground Control looks up satellite by SPIFFE ID
6. Ground Control returns state config (no token exchange needed)

### Phase 4: Authorization with OPA (Optional)

Integrate Open Policy Agent for fine-grained authorization:
```rego
package satellite.authz

default allow = false

allow {
    input.spiffe_id == sprintf("spiffe://harbor-satellite.example.org/satellite/%s", [input.satellite_name])
    satellite_exists(input.satellite_name)
    satellite_has_group_access(input.satellite_name, input.requested_groups)
}
```

---

## Code Changes Overview

### Ground Control: SPIFFE Server Integration

```go
// ground-control/internal/spiffe/source.go
package spiffe

import (
    "github.com/spiffe/go-spiffe/v2/workloadapi"
)

type SPIFFESource struct {
    x509Source *workloadapi.X509Source
}

func NewSPIFFESource(socketPath string) (*SPIFFESource, error) {
    source, err := workloadapi.NewX509Source(
        context.Background(),
        workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)),
    )
    return &SPIFFESource{x509Source: source}, err
}
```

### Ground Control: mTLS Server Config

```go
// In server.go - buildServerTLSConfig
func buildSPIFFETLSConfig(source *spiffe.SPIFFESource) (*tls.Config, error) {
    return tlsconfig.MTLSServerConfig(
        source.x509Source,
        source.x509Source,
        tlsconfig.AuthorizeAny(),  // Or custom authorizer
    )
}
```

### Satellite: SPIFFE Client

```go
// internal/spiffe/client.go
package spiffe

import (
    "github.com/spiffe/go-spiffe/v2/workloadapi"
    "github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
)

func NewTLSConfig(socketPath string) (*tls.Config, error) {
    source, err := workloadapi.NewX509Source(
        context.Background(),
        workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)),
    )
    if err != nil {
        return nil, err
    }

    return tlsconfig.MTLSClientConfig(
        source,
        source,
        tlsconfig.AuthorizeAny(),
    ), nil
}
```

---

## Configuration Changes

### Ground Control Environment Variables

```env
# Existing TLS (still supported as fallback)
TLS_CERT_FILE=/path/to/cert.pem
TLS_KEY_FILE=/path/to/key.pem

# SPIFFE Configuration
SPIFFE_ENABLED=true
SPIFFE_TRUST_DOMAIN=acme.prod.harbor-satellite

# Provider: "sidecar" (default) or "external"
SPIFFE_PROVIDER=sidecar

# Sidecar provider config (default)
SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock

# External provider config (enterprise)
# SPIFFE_PROVIDER=external
# SPIFFE_EXTERNAL_SERVER_ADDRESS=spire-server.corp.example.com:8081
# SPIFFE_EXTERNAL_TRUST_BUNDLE_PATH=/etc/spire/bundle.pem

# Auth mode: "spiffe", "token", or "both" (default)
AUTH_MODE=both
```

### Satellite Configuration

```json
{
  "app_config": {
    "ground_control_url": "https://ground-control:8080",
    "auth": {
      "mode": "spiffe",
      "spiffe": {
        "enabled": true,
        "endpoint_socket": "unix:///run/spire/sockets/agent.sock",
        "expected_server_id": "spiffe://acme.prod.harbor-satellite/gc/main"
      },
      "token": {
        "value": ""
      }
    }
  }
}
```

### CLI Flags

```bash
# Satellite
--auth-mode=spiffe|token   # Authentication method (default: token for backward compat)
--spiffe-socket=PATH       # SPIFFE Workload API socket path
--spiffe-server-id=ID      # Expected Ground Control SPIFFE ID

# Ground Control
--spiffe-provider=sidecar|external
--spiffe-socket=PATH
--auth-mode=spiffe|token|both
```

---

## SPIRE Registration Entries

### Ground Control Service
```bash
spire-server entry create \
  -spiffeID spiffe://harbor-satellite.example.org/ground-control \
  -parentID spiffe://harbor-satellite.example.org/spire-agent \
  -selector k8s:ns:harbor-satellite \
  -selector k8s:sa:ground-control
```

### Satellite Workload (per satellite)
```bash
spire-server entry create \
  -spiffeID spiffe://harbor-satellite.example.org/satellite/edge-site-1 \
  -parentID spiffe://harbor-satellite.example.org/spire-agent \
  -selector unix:uid:1000 \
  -selector docker:label:satellite-name:edge-site-1
```

---

## Migration Strategy

### Backward Compatibility
1. Default `AUTH_MODE=both` allows token and SPIFFE simultaneously
2. Token auth remains available for:
   - Gradual migration of existing satellites
   - Air-gapped deployments without SPIRE
   - Simple testing/development setups
3. New satellites can use either method based on deployment

### Rollout Steps
1. **Phase 1**: Deploy Ground Control with SPIFFE support (dual-mode)
2. **Phase 2**: Deploy SPIRE Server sidecar with Ground Control
3. **Phase 3**: Deploy SPIRE Agents to edge nodes
4. **Phase 4**: Migrate satellites one-by-one to SPIFFE auth
5. **Phase 5**: (Optional) Disable token auth for SPIFFE-only mode

---

## Files to Create/Modify

### New Files
| Path | Purpose |
|------|---------|
| `ground-control/internal/spiffe/source.go` | SPIFFE X509Source wrapper |
| `ground-control/internal/spiffe/authorizer.go` | Custom SPIFFE ID authorizer |
| `ground-control/internal/spiffe/provider.go` | SPIREProvider interface (sidecar/external) |
| `internal/spiffe/client.go` | Satellite SPIFFE client |
| `internal/spiffe/embedded/agent.go` | Embedded SPIRE agent (build tag: embedded_spire) |
| `internal/spiffe/embedded/config.go` | Embedded agent configuration |
| `internal/spiffe/embedded/attestor_jointoken.go` | Join token attestor |
| `internal/spiffe/embedded/attestor_tpm.go` | TPM attestor |
| `internal/spiffe/embedded/attestor_x509pop.go` | X.509 PoP attestor (lazy mode) |
| `deploy/spire/` | SPIRE deployment manifests |
| `deploy/spire/server.conf` | SPIRE Server config template |
| `deploy/spire/agent.conf` | SPIRE Agent config template |

### Modified Files
| Path | Changes |
|------|---------|
| `ground-control/internal/server/server.go` | Add SPIFFE TLS option |
| `ground-control/internal/server/satellite_handlers.go` | Extract SPIFFE ID from cert |
| `ground-control/internal/server/routes.go` | Add SPIFFE-authenticated routes |
| `internal/state/registration_process.go` | Use SPIFFE for registration |
| `internal/tls/config.go` | Add SPIFFE config loading |
| `pkg/config/config.go` | Add SPIFFE config structs |
| `cmd/main.go` | Add SPIFFE flags |

---

## Verification Plan

1. **Unit Tests**: Mock SPIFFE source, test TLS config creation
2. **Integration Tests**:
   - Start SPIRE server/agent in test containers
   - Register test entries
   - Verify mTLS connection works
3. **E2E Tests**:
   - Deploy full SPIRE + Harbor Satellite stack
   - Test satellite registration via SPIFFE
   - Test state replication with SPIFFE auth
4. **Manual Testing**:
   - Verify certificate rotation (SVID refresh)
   - Test with invalid/expired SVIDs
   - Test authorization policies

---

## Dependencies

```go
// go.mod additions
require (
    github.com/spiffe/go-spiffe/v2 v2.x.x
)
```

---

## Federation Support (Future)

The SPIFFE ID scheme is designed to support federation:

```
# Trust bundle exchange between environments
spire-server bundle show -format spiffe > staging-bundle.json
spire-server bundle set -format spiffe -id spiffe://acme.staging.harbor-satellite < staging-bundle.json

# Satellite registration with federation
spire-server entry create \
  -spiffeID spiffe://acme.prod.harbor-satellite/satellite/region/us-west/edge-1 \
  -parentID spiffe://acme.prod.harbor-satellite/spire-agent \
  -selector unix:uid:1000 \
  -federatesWith spiffe://acme.staging.harbor-satellite
```

Federation enables:
- Cross-environment trust (prod <-> staging)
- Partner organization integration
- Multi-region deployment with separate trust domains

---

## Final Decisions

| Decision | Choice |
|----------|--------|
| SPIFFE ID Structure | `satellite/region/{region}/{name}` |
| Join Token Distribution | Both CLI and API endpoint |

### Join Token API Endpoint

Ground Control will expose an endpoint for automated join token generation:

```
POST /satellites/{name}/join-token
Authorization: Bearer <admin-token>

Response:
{
  "join_token": "abc123...",
  "expires_at": "2024-01-20T12:00:00Z",
  "spiffe_id": "spiffe://acme.prod.harbor-satellite/satellite/region/us-west/edge-1"
}
```

This endpoint will:
1. Generate SPIRE join token via SPIRE Server API
2. Create registration entry for the satellite
3. Return token for satellite bootstrap

Manual CLI remains available for air-gapped or enterprise SPIRE deployments.

---

## Future Enhancement: OIDC Federation for Cloud Storage

SPIRE's OIDC Discovery Provider can enable keyless authentication to cloud services. This could be used for:

**Use Case**: Satellite authenticates with S3/GCS/Azure Blob without static credentials

```
# Satellite with JWT-SVID can assume AWS IAM role
AWS_ROLE_ARN=arn:aws:iam::123456789:role/satellite-storage
AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/spire/jwt-svid

# Access S3 bucket for artifact storage
aws s3 cp s3://harbor-artifacts/image.tar ./
```

**Benefits**:
- No static AWS credentials in satellite config
- Short-lived tokens (5 min expiry)
- SPIFFE ID-based access control in IAM policies
- Works with S3, STS, and other AWS services

**Implementation** (deferred):
1. Deploy SPIRE OIDC Discovery Provider
2. Configure AWS IAM Identity Provider to trust SPIRE
3. Create IAM roles with SPIFFE ID conditions
4. Modify Zot registry config to use OIDC-based S3 auth

Reference: https://spiffe.io/docs/latest/keyless/oidc-federation-aws/
