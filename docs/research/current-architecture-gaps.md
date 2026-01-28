# Harbor Satellite: Current Architecture & Security Gaps

Analysis of the current Harbor Satellite implementation and identified security gaps.

## Current Registration Flow

```
┌──────────────┐                    ┌─────────────────┐                    ┌────────────┐
│   Satellite  │                    │  Ground Control │                    │   Harbor   │
└──────┬───────┘                    └────────┬────────┘                    └─────┬──────┘
       │                                     │                                   │
       │  1. Start with --token              │                                   │
       │     --ground-control-url            │                                   │
       │                                     │                                   │
       │  2. GET /satellites/ztr/{token}     │                                   │
       │────────────────────────────────────>│                                   │
       │                                     │                                   │
       │                                     │  3. Validate token in DB          │
       │                                     │     (SatelliteToken table)        │
       │                                     │                                   │
       │                                     │  4. Lookup robot credentials      │
       │                                     │     (RobotAccount table)          │
       │                                     │                                   │
       │  5. Return StateConfig              │                                   │
       │<────────────────────────────────────│                                   │
       │     - registry URL                  │                                   │
       │     - robot username/password       │                                   │
       │     - state artifact URL            │                                   │
       │                                     │                                   │
       │                                     │  6. Delete token from DB          │
       │                                     │     (single-use)                  │
       │                                     │                                   │
       │  7. Store StateConfig to config.json│                                   │
       │     (PLAINTEXT!)                    │                                   │
       │                                     │                                   │
       │  8. Periodic state replication ─────│───────────────────────────────────>│
       │     (using robot credentials)       │                                   │
```

### Key Files
- `cmd/main.go`: CLI entry point
- `internal/state/registration_process.go`: ZTR flow
- `pkg/config/`: Configuration management

## Current State Replication

```
┌──────────────┐                                      ┌────────────┐
│   Satellite  │                                      │   Harbor   │
└──────┬───────┘                                      └─────┬──────┘
       │                                                    │
       │  Every 10s (configurable):                         │
       │                                                    │
       │  1. Pull satellite state artifact                  │
       │────────────────────────────────────────────────────>│
       │     (Basic Auth: robot creds)                      │
       │                                                    │
       │<────────────────────────────────────────────────────│
       │     artifacts.json (list of groups)                │
       │                                                    │
       │  2. For each group (parallel):                     │
       │     Pull group state artifact                      │
       │────────────────────────────────────────────────────>│
       │                                                    │
       │<────────────────────────────────────────────────────│
       │     artifacts to replicate                         │
       │                                                    │
       │  3. Diff: added/removed/modified                   │
       │                                                    │
       │  4. Copy new artifacts to local registry           │
       │────────────────────────────────────────────────────>│
       │     (Crane library)                                │
       │                                                    │
       ▼  5. Delete removed artifacts locally               │
```

### Key Files
- `internal/state/state_process.go`: Orchestration
- `internal/state/fetcher.go`: Artifact pulling
- `internal/state/replicator.go`: Artifact copying

---

## Credential Storage (Current)

### Bootstrap Token
- Generated: `GenerateRandomToken(32)` - 256-bit random, hex-encoded
- Storage: Database `SatelliteToken` table
- Lifecycle: Single-use, deleted after ZTR

### Robot Account Credentials
- Generated: By Harbor on satellite registration
- Storage (DB): `RobotAccount` table (plaintext)
- Storage (Edge): `config.json` as plaintext
- Lifecycle: Never rotated

### Config File Structure
```json
{
  "state_config": {
    "auth": {
      "username": "robot$satellite-xxx",
      "password": "abc123..."
    },
    "url": "https://harbor.example.com/satellite/state"
  },
  "app_config": {
    "ground_control_url": "https://gc.example.com",
    "log_level": "info",
    "use_unsecure": false
  }
}
```

---

## Security Gaps

### Critical

| Gap | Current State | Risk | Impact |
|-----|---------------|------|--------|
| Credential Storage | Plaintext in config.json | File access = full compromise | Attacker gets Harbor access |
| TLS Optional | `use_unsecure` flag allows HTTP | MITM attacks | Credential theft |
| No Encryption at Rest | Config file unencrypted | Physical access compromise | Full satellite takeover |

### High

| Gap | Current State | Risk | Impact |
|-----|---------------|------|--------|
| No mTLS | Only server TLS | No client authentication | Impersonation possible |
| No Credential Rotation | Robot creds never change | Long exposure window | Stale creds remain valid |
| DB Credentials Plaintext | Robot secrets unencrypted | DB breach = all creds | Fleet-wide compromise |
| No Rate Limiting | Unlimited ZTR attempts | Token brute force | Unauthorized registration |

### Medium

| Gap | Current State | Risk | Impact |
|-----|---------------|------|--------|
| No Request Signing | Requests not authenticated | Tampering | State manipulation |
| No Audit Logging | No credential usage logs | No visibility | Cannot detect misuse |
| Ground Control Unauthenticated | No API authentication | Open management API | Unauthorized control |

---

## Threat Model

### Threats NOT Mitigated

**1. Man-in-the-Middle**
- No TLS enforcement between satellite and Ground Control/Harbor
- `use_unsecure` flag allows HTTP
- Credentials transmitted in clear text

**2. Compromised Satellite**
- Plaintext credentials in `config.json`
- File readable by any local user
- No encryption at rest

**3. Database Breach**
- Robot credentials stored as plaintext
- Compromise of Ground Control DB = all credentials exposed
- No encryption, no hashing

**4. Token Brute Force**
- No rate limiting on ZTR endpoint
- Token is only 64 hex characters
- Automated attacks possible

**5. Configuration Tampering**
- No signature verification on state artifacts
- Tampered config could redirect satellite
- No integrity checks

**6. Replay Attacks**
- No nonce/timestamp validation
- Captured requests can be replayed
- No request uniqueness

**7. Credential Exposure in Logs**
- Debug logging may expose credentials
- No redaction of sensitive data

**8. Privilege Escalation**
- CRI config updated with sudo
- Satellite binary may have elevated privileges

---

## Zero-Trust Requirements (Not Met)

### Authentication
- [ ] mTLS between components
- [ ] Certificate-based identity
- [ ] Request signing (HMAC, Ed25519)
- [ ] Workload identity binding (SPIFFE/SPIRE)
- [ ] OAuth2/OIDC integration
- [ ] Ground Control API authentication

### Secrets Management
- [ ] Encrypted storage at rest
- [ ] Key derivation functions
- [ ] Secrets rotation
- [ ] Secret versioning
- [ ] Audit trail for credential access
- [ ] External secrets provider integration

### Network Security
- [ ] Mandatory TLS
- [ ] Certificate pinning
- [ ] Network policies
- [ ] mTLS for local registry

### Runtime Security
- [ ] Capability dropping/SECCOMP
- [ ] Security contexts
- [ ] Secret scanning
- [ ] Integrity verification

---

## Communication Patterns

### Satellite to Ground Control
- `GET /satellites/ztr/{token}` - ZTR (unauthenticated, token in URL)
- No other satellite-initiated calls

### Satellite to Harbor
- Basic Auth with robot credentials
- Pulls state artifacts (OCI)
- Copies images to local registry (Crane)

### Ground Control to Satellite
- NONE - fully pull-based
- No way to push commands
- Must wait for satellite to poll

---

## Database Schema

### Key Tables

**Satellite**
- satellite_id, name, created_at, updated_at

**SatelliteToken**
- token, satellite_id
- One-time use, deleted after ZTR

**RobotAccount**
- robot_name, robot_secret (PLAINTEXT), robot_id, satellite_id

**SatelliteGroup**
- satellite_id, group_id (many-to-many)

**Group**
- group_name, registry_url, projects[], created_at

---

## Recommended Fixes by Priority

### Immediate (P0)
1. **Encrypt config.json** - Device-derived key
2. **Enforce TLS** - Remove `use_unsecure` option
3. **Encrypt DB credentials** - Use encryption at rest

### Short-term (P1)
1. **Implement mTLS** - Client certificate authentication
2. **Add credential rotation** - Periodic robot credential refresh
3. **Add rate limiting** - Protect ZTR endpoint
4. **Add audit logging** - Track all operations

### Medium-term (P2)
1. **SPIFFE/SPIRE integration** - Workload identity
2. **Request signing** - Integrity verification
3. **Certificate pinning** - Prevent MITM
4. **Ground Control auth** - API authentication

---

## Sources

- Codebase analysis: `internal/state/`, `pkg/config/`, `cmd/main.go`
- Ground Control: `ground-control/internal/server/`, `ground-control/internal/database/`
